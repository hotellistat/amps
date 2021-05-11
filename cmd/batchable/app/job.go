package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"encoding/json"
	"io/ioutil"
	"net/http"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// JobCheckout is executed when a workload finishes a job, and registers it as completed
func JobComplete(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) {

	event := cloudevents.NewEvent()

	body, readErr := ioutil.ReadAll(req.Body)

	if readErr != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Could not read request Body"))
		return
	}

	event, err := cloudevent.Unmarshal(body)

	if err != nil {
		println("[batchable]", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Can not unmarshal cloudevent, make sure you send a cloudevent in structured content mode"))
		return
	}

	eventID := event.Context.GetID()

	jobManifest.Mutex.Lock()

	if !jobManifest.HasJob(eventID) {
		jobManifest.Mutex.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Could not publish your event to the broker. Job may have timed out."))
		println("[batchable] Job ID:", eventID, "does not exists anymore. Publishing blocked.")
		return
	}

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore
	nopublish, _ := event.Context.GetExtension("nopublish")

	jobManifest.DeleteJob(eventID)

	startBroker := jobManifest.Size() < conf.MaxConcurrency

	jobManifest.Mutex.Unlock()

	if startBroker {
		(*broker).Start()
	}

	if nopublish != true {
		if conf.Debug {
			println("[batchable] Publishing recieved event to broker")
		}
		publishErr := (*broker).PublishMessage(event)
		if publishErr != nil {
			println("[batchable] Could not publish event to broker", publishErr.Error())
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Could not publish your event to the broker"))
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
}

type JobDeleteSchema struct {
	Identifier string
}

func JobDelete(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim) {

	var job JobDeleteSchema

	err := json.NewDecoder(req.Body).Decode(&job)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobManifest.Mutex.Lock()

	if !jobManifest.HasJob(job.Identifier) {
		jobManifest.Mutex.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Could not publish job. Job does not exists in the manifest."))
		println("[batchable] Job ID:", job.Identifier, "does not exists in the manifest")
		return
	}

	// Fetch the nopublish event context extension. This will prevent publishing
	// the recieved event to our broker. This is normally used,
	// if you want to define the end of a chain of workloads where the last
	// link of the chain should not create any new events in the broker anymore

	jobManifest.DeleteJob(job.Identifier)

	startBroker := jobManifest.Size() < conf.MaxConcurrency

	jobManifest.Mutex.Unlock()

	if startBroker {
		(*broker).Start()
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
}
