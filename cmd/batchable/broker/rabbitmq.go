package broker

import (
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"

	"github.com/cloudevents/sdk-go/v2/event"
)

// AMQPBroker represents the primary natsshim communication instance
type AMQPBroker struct {
	config config.Config
}

// Initialize creates a new natsshim connection
func (b *AMQPBroker) Initialize(config config.Config) {
	b.config = config

}

// Teardown the natsshim connection and all natsshim services
func (b *AMQPBroker) Teardown() {

}

// Start creates a new subscription and executes the messageCallback on new messages
func (b *AMQPBroker) Start(jobManifest *job.Manifest) {

}

// Stop closes the natsshim subscription so no new messages will be recieved
func (b *AMQPBroker) Stop() {

}

// PublishResult result will publish the worker result to the message queue
func (b *AMQPBroker) PublishResult(config config.Config, event event.Event) error {

	return nil
}

// Healthy checks the health of the broker
func (b *AMQPBroker) Healthy() bool {

	return true
}
