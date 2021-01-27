package app

import (
	"batchable/cmd/batchable/config"
	"encoding/json"

	cloudevent "github.com/cloudevents/sdk-go/v2"
	"github.com/nats-io/stan.go"
)

// MessageHandler will execute on every new borker message
func MessageHandler(
	msg *stan.Msg,
	conf *config.Config,
	jobManifest *JobManifest,
	broker *BrokerShim) {

	event := cloudevent.NewEvent()

	err := json.Unmarshal(msg.Data, &event)

	if err != nil {
		println("Could not Marshal Cloud Event", string(msg.Data))
		msg.Ack()
		println(err.Error())
		return
	}

	eventID := event.Context.GetID()

	if conf.Debug {
		println("Job ID:", eventID)
	}

	// FlagStropBroker represents a flat that is set, so that a condition outside of our lock can evaluate
	// if the broker should be stopped. This is important, because we want to acknowledge the message before
	// the subscription is stopped, otherwise the broker may want to resend the message becaus a ack could not
	// be sent on a closed connection anymore. And because we want our mutex to be as performant as possible,
	// we want to execute the Acknowledgement outside of the mutext since the ack is blocking/sychronous.
	flagStopBroker := false

	jobManifest.Lock()

	insertErr := jobManifest.InsertJob(eventID)

	if insertErr != nil {
		println(insertErr.Error())
		return
	}

	if jobManifest.Size() >= conf.MaxConcurrency {
		if conf.Debug {
			println("Max job concurrency reached, stopping broker")
		}
		flagStopBroker = true
	}

	jobManifest.Unlock()

	msg.Ack()

	if flagStopBroker {
		(*broker).Stop()
	}

	workloadErr := TriggerWorkload(event, *conf)

	if workloadErr != nil {
		println(workloadErr.Error())
	}
}
