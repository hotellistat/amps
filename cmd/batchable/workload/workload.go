package workload

import (
	"batchable/cmd/batchable/config"
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/cloudevents/sdk-go/v2/event"
)

// TriggerWorkload sends a HTTP request to the sibling workload on every new broker job
func Trigger(
	event event.Event,
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

	body, _ := ioutil.ReadAll(resp.Body)
	println("[batchable] response", string(body))

	return nil
}
