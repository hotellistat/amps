package workload

import (
	"batchable/cmd/batchable/config"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	cloudeventSdk "github.com/cloudevents/sdk-go/v2/event"
)

// TriggerWorkload sends a HTTP request to the sibling workload on every new broker job
func Trigger(
	event cloudeventSdk.Event,
	conf config.Config) error {

	eventData, err := json.Marshal(event)

	if err != nil {
		return errors.New("could not marshal cloudevent for workload")
	}

	client := http.Client{
		Timeout: conf.WorkloadResponseTimeout,
	}

	resp, err := client.Post(conf.WorkloadAddress, "application/json", bytes.NewBuffer(eventData))
	if err != nil {
		return err
	}

	resp.Body.Close()

	println("[batchable] status code", resp.StatusCode)

	return nil
}
