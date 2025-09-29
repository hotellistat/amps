package app

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/hotellistat/amps/cmd/amps/broker"
	"github.com/hotellistat/amps/cmd/amps/cloudevent"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	workloadsAcknowledged = promauto.NewCounter(prometheus.CounterOpts{
		Name: "amps_workloads_acknowledged_total",
		Help: "The total number of workloads job acknoldegdenemts. This means that a workload has successfully completed a job.",
	})
	workloadsRejected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "amps_workloads_rejected_total",
		Help: "The total number of workloads job rejections. This means that a workload has crashed or has thrown an exception notified the amps container of an unsuccessful job completion.",
	})
	workloadsPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "amps_workloads_published_total",
		Help: "The total number of message publishings that were triggered by a workload.",
	})
)

// JobCheckout is executed when a workload finishes a job, and registers it as completed
func JobPublish(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) error {

	body, readErr := ioutil.ReadAll(req.Body)

	if readErr != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Could not read request Body"))
		return readErr
	}

	event, err := cloudevent.Unmarshal(body)

	if err != nil {
		println("[AMPS]", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Can not unmarshal cloudevent, make sure you send a cloudevent in structured content mode"))
		return err
	}

	eventID := event.ID()
	println("[AMPS] Publishing Job:", eventID)

	publishErr := (*broker).PublishMessage(event)
	if publishErr != nil {
		println("[AMPS] Could not publish event to broker", publishErr.Error())
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not publish your event to the broker"))
		return publishErr
	}

	workloadsPublished.Inc()
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
	return nil
}

type JobSchema struct {
	Identifier string `json:"identifier"`
	Reschedule bool   `json:"reschedule,omitempty"`
}

// JobCheckout is executed when a workload finishes a job, and registers it as completed
func JobAcknowledge(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) error {

	job := JobSchema{
		Reschedule: false,
	}

	err := json.NewDecoder(req.Body).Decode(&job)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	jobManifest.Mutex.Lock()

	// Check if job exists in the manifest
	if !jobManifest.HasJob(job.Identifier) {
		jobManifest.Mutex.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Could not publish job. Job does not exist in the manifest."))
		println("[AMPS] Job ID:", job.Identifier, "does not exist in the manifest")
		return errors.New("Job ID: " + job.Identifier + " does not exist in the manifest")
	}

	// Get the job to access its AMQP delivery for acknowledgment
	jobItem := jobManifest.GetJob(job.Identifier)

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	jobManifest.DeleteJob(job.Identifier)
	jobManifest.Mutex.Unlock()

	// Handle the message based on reschedule flag
	if jobItem.Delivery != nil {
		if job.Reschedule {
			// Reschedule = NACK with requeue (put message back in queue)
			nackErr := jobItem.Delivery.Nack(false, true)
			if nackErr != nil {
				// Check if this is a stale delivery from a reconnected connection
				if nackErr.Error() == "Exception (504) Reason: \"channel/connection is not open\"" {
					println("[AMPS] message reschedule skipped - connection was reconnected, message will be redelivered")
				} else {
					println("[AMPS] error rescheduling RabbitMQ message:", nackErr.Error())
				}
			} else {
				println("[AMPS] job", job.Identifier, "rescheduled successfully")
			}
		} else {
			// Normal acknowledgment - message processed successfully
			workloadsAcknowledged.Inc()
			ackErr := jobItem.Delivery.Ack(false)
			if ackErr != nil {
				// Check if this is a stale delivery from a reconnected connection
				if ackErr.Error() == "Exception (504) Reason: \"channel/connection is not open\"" {
					println("[AMPS] message acknowledgment skipped - connection was reconnected, message will be redelivered")
				} else {
					println("[AMPS] error acknowledging RabbitMQ message:", ackErr.Error())
				}
			}
		}
	} else {
		// No delivery to acknowledge (shouldn't happen in normal operation)
		if !job.Reschedule {
			workloadsAcknowledged.Inc()
		}
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
	return nil
}

func JobReject(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) error {

	job := JobSchema{
		Reschedule: false,
	}

	err := json.NewDecoder(req.Body).Decode(&job)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	jobManifest.Mutex.Lock()

	// Check if job exists in the manifest
	if !jobManifest.HasJob(job.Identifier) {
		jobManifest.Mutex.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Could not publish job. Job does not exist in the manifest."))
		println("[AMPS] Job ID:", job.Identifier, "does not exist in the manifest")
		return errors.New("Job ID: " + job.Identifier + " does not exist in the manifest")
	}

	// Get the job to access its AMQP delivery for nack
	jobItem := jobManifest.GetJob(job.Identifier)

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	jobManifest.DeleteJob(job.Identifier)
	workloadsRejected.Inc()
	jobManifest.Mutex.Unlock()

	// Nack the RabbitMQ message - requeue behavior depends on reschedule flag
	if jobItem.Delivery != nil {
		// job.Reschedule determines whether to requeue (true) or discard (false)
		nackErr := jobItem.Delivery.Nack(false, job.Reschedule)
		if nackErr != nil {
			// Check if this is a stale delivery from a reconnected connection
			if nackErr.Error() == "Exception (504) Reason: \"channel/connection is not open\"" {
				println("[AMPS] message nack skipped - connection was reconnected, message will be redelivered")
			} else {
				println("[AMPS] error nacking RabbitMQ message:", nackErr.Error())
			}
		} else {
			if job.Reschedule {
				println("[AMPS] job", job.Identifier, "rejected and rescheduled")
			} else {
				println("[AMPS] job", job.Identifier, "rejected and discarded")
			}
		}
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
	return nil
}
