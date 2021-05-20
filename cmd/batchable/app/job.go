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

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// JobCheckout is executed when a workload finishes a job, and registers it as completed
func JobPublish(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) error {

	event := cloudevents.NewEvent()

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

	publishErr := (*broker).PublishMessage(event)
	if publishErr != nil {
		println("[batchable] Could not publish event to broker", publishErr.Error())
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not publish your event to the broker"))
		return publishErr
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
	return nil
}

type JobSchema struct {
	Identifier string
}

// JobCheckout is executed when a workload finishes a job, and registers it as completed
func JobAcknowledge(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) error {

	var job JobSchema

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

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	jobManifest.DeleteJob(job.Identifier)
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

	var job JobSchema

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

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	jobManifest.DeleteJob(job.Identifier)
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
