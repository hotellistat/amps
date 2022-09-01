package eventfcns

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

const (
	PublishUrlTemplate = "http://localhost%s/publish"
)

func PublishEvent(event cloudevents.Event, masterResponsePort string) error {
	var err error = nil
	var eventBytes []byte
	var response *http.Response

	masterPublishUrl := fmt.Sprintf(PublishUrlTemplate, masterResponsePort)
	if eventBytes, err = json.Marshal(event); err != nil {
		log.Printf("ERROR in Marshall(event): %v\n", err)
		return err
	}
	log.Printf("SendEvent for eventID: %s\n", event.ID())
	if response, err = http.Post(masterPublishUrl, "text/plain", bytes.NewBuffer(eventBytes)); err != nil {
		log.Printf("ERROR in publish request: %v\n", err)
		return err
	}
	if response != nil && response.StatusCode != http.StatusOK {
		err = errors.New(fmt.Sprintf("ERROR response.StatusCode: %v err: %v\n", response.StatusCode, err))
		log.Printf("%v\n", err)
		return err
	}
	return err
}

func SendEvent(port string, source string, eventType string, eventID string, eventDataValues interface{}) error {
	var err error = nil
	var event cloudevents.Event
	var eventDataBytes []byte

	// Create an Event.
	event = cloudevents.NewEvent()
	event.SetSource(source)
	event.SetID(eventID)
	event.SetType(eventType)
	if eventDataBytes, err = json.Marshal(eventDataValues); err != nil {
		log.Printf("ERROR in Marshall(eventDataValues): %v\n", err)
		return err
	}
	event.SetData(cloudevents.ApplicationJSON, eventDataBytes)
	if PublishEvent(event, port); err != nil {
		log.Printf("ERROR in SendEvent: %v\n", err)
		return err
	}
	return err
}
