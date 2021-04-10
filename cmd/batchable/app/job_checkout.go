package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"io/ioutil"
	"net/http"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// JobCheckout is executed when a workload finishes a job, and registers it as completed
func JobCheckout(
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
		println(err.Error())
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Can not unmarshal cloudevent, make sure you send a cloudevent in structured content mode"))
		return
	}

	eventID := event.Context.GetID()

	if !jobManifest.HasJob(eventID) && conf.ContainZombieJobs {
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("Could not publish your event to the broker. Job may have timed out."))
		println("Job ID:", eventID, "does not exists anymore. Publishing blocked.")
		return
	}

	// Fetch the nopublish event context extension. This will prevent publishing the recieved event to our broker.
	// This is normally used, if you want to define the end of a chain of workloads, where the last link of the chain
	// Should not create any new events in the broker anymore
	nopublish, _ := event.Context.GetExtension("nopublish")

	if conf.Debug {
		println("Deleting Job ID:", eventID)
	}

	if nopublish != true {
		if conf.Debug {
			println("Publishing recieved event to broker")
		}
		publishErr := (*broker).PublishMessage(event)
		if publishErr != nil {
			println("Could not publish event to broker")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Could not publish your event to the broker"))
			return
		}
	}

	if !conf.InstantAck {
		jobManifest.AcknowlegeJob(eventID)
	}

	jobManifest.DeleteJob(eventID)

	if jobManifest.Size() < conf.MaxConcurrency {
		// Initialize a new subscription should the old one have been closed
		(*broker).Start()
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
}
