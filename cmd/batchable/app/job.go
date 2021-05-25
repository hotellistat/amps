package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	workloadsAcknowledged = promauto.NewCounter(prometheus.CounterOpts{
		Name: "batchable_workloads_acknowledged_total",
		Help: "The total number of workloads job acknoldegdenemts. This means that a workload has successfully completed a job.",
	})
	workloadsRejected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "batchable_workloads_rejected_total",
		Help: "The total number of workloads job rejections. This means that a workload has crashed or has thrown an exception notified the batchable container of an unsuccessful job completion.",
	})
	workloadsPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "batchable_workloads_published_total",
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
		println("[batchable]", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Can not unmarshal cloudevent, make sure you send a cloudevent in structured content mode"))
		return err
	}

	eventID := event.ID()
	println("[batchable] Publishing Job:", eventID)

	publishErr := (*broker).PublishMessage(event)
	if publishErr != nil {
		println("[batchable] Could not publish event to broker", publishErr.Error())
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
		println("[batchable] Job ID:", job.Identifier, "does not exists in the manifest")
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
		println("[batchable] Job ID:", job.Identifier, "does not exists in the manifest")
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
