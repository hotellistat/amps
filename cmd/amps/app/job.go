package app

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/hotellistat/AMPS/cmd/amps/broker"
	"github.com/hotellistat/AMPS/cmd/amps/cloudevent"
	"github.com/hotellistat/AMPS/cmd/amps/config"
	"github.com/hotellistat/AMPS/cmd/amps/job"
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
		w.Write([]byte("Could not publish job. Job does not exists in the manifest."))
		println("[AMPS] Job ID:", job.Identifier, "does not exists in the manifest")
		return errors.New("Job ID: " + job.Identifier + " does not exists in the manifest")
	}

	if job.Reschedule {
		jobMessage := jobManifest.GetJob(job.Identifier)
		newEvent, _ := cloudevent.Unmarshal(jobMessage.Message.GetData())
		go (*broker).PublishMessage(newEvent)
	}

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	jobManifest.DeleteJob(job.Identifier)
	workloadsAcknowledged.Inc()
	startBroker := jobManifest.Size() < conf.MaxConcurrency
	jobManifest.Mutex.Unlock()

	if startBroker {
		startErr := (*broker).Start()
		if startErr != nil {
			return startErr
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
		w.Write([]byte("Could not publish job. Job does not exists in the manifest."))
		println("[AMPS] Job ID:", job.Identifier, "does not exists in the manifest")
		return errors.New("Job ID: " + job.Identifier + " does not exists in the manifest")
	}

	if job.Reschedule {
		jobMessage := jobManifest.GetJob(job.Identifier)
		newEvent, _ := cloudevent.Unmarshal(jobMessage.Message.GetData())
		go (*broker).PublishMessage(newEvent)
	}

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	jobManifest.DeleteJob(job.Identifier)
	workloadsRejected.Inc()
	startBroker := jobManifest.Size() < conf.MaxConcurrency
	jobManifest.Mutex.Unlock()

	if startBroker {
		startErr := (*broker).Start()
		if startErr != nil {
			return startErr
		}
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
	return nil
}
