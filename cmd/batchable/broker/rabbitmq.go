package broker

import (
	"batchable/cmd/batchable/config"

	"github.com/cloudevents/sdk-go/v2/event"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/stan.go"
)

// RabbitMqBroker represents the primary natsshim communication instance
type RabbitMqBroker struct {
	config         config.Config
	natsConnection *nats.Conn
	stanConnection stan.Conn
	subscription   stan.Subscription
}

// Initialize creates a new natsshim connection
func (n *RabbitMqBroker) Initialize(config config.Config) {

}

// Teardown the natsshim connection and all natsshim services
func (n *RabbitMqBroker) Teardown() {

}

// Start creates a new subscription and executes the messageCallback on new messages
func (n *RabbitMqBroker) Start(messageCallback stan.MsgHandler) {

}

// Stop closes the natsshim subscription so no new messages will be recieved
func (n *RabbitMqBroker) Stop() {

}

// PublishResult result will publish the worker result to the message queue
func (n *RabbitMqBroker) PublishResult(config config.Config, event event.Event) error {

	return nil
}

// Healthy checks the health of the broker
func (n *RabbitMqBroker) Healthy() bool {

	return true
}
