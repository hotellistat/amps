package kafkashim

import (
	"batchable/internal/config"

	"github.com/nats-io/stan.go"
)

// KafkaBroker represents the primary kafkashim communication instance
type KafkaBroker struct {
}

// Initialize creates a new kafkashim connection
func (n *KafkaBroker) Initialize(config config.Config) {

}

// Teardown the kafkashim connection and all kafkashim services
func (n *KafkaBroker) Teardown() {

}

// Start creates a new subscription and executes the messageCallback on new messages
func (n *KafkaBroker) Start(messageCallback stan.MsgHandler) {

}

// Stop closes the kafkashim subscription so no new messages will be recieved
func (n *KafkaBroker) Stop() {

}

// PublishResult result will publish the worker result to the message queue
func (n *KafkaBroker) PublishResult(config config.Config, msg []byte) error {
	return nil
}
