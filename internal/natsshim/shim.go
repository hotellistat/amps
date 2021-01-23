package natsshim

import (
	"batchable/internal/config"
	"errors"
	"log"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/stan.go"
)

// NatsBroker represents the primary natsshim communication instance
type NatsBroker struct {
	config         config.Config
	natsConnection *nats.Conn
	stanConnection stan.Conn
	subscription   stan.Subscription
}

// Initialize creates a new natsshim connection
func (n *NatsBroker) Initialize(config config.Config) {
	n.config = config

	log.Println(config.NatsHost, config.NatsCluster)

	nc, err := nats.Connect(config.NatsHost)
	if err != nil {
		log.Fatal("Could not connect to NATS")
	}

	log.Println("Worker ID:", config.WorkerID)

	sc, err := stan.Connect(config.NatsCluster, config.WorkerID, stan.NatsConn(nc), stan.SetConnectionLostHandler(func(_ stan.Conn, reason error) {
		log.Fatalf("Connection lost, reason: %v", reason)
	}))

	if err != nil {
		log.Fatal("Could not connect to NATS Streaming server")
		log.Fatal(err)
	}

	n.natsConnection = nc
	n.stanConnection = sc
}

// Teardown the natsshim connection and all natsshim services
func (n *NatsBroker) Teardown() {
	if n.subscription != nil {
		n.subscription.Close()
	}
	n.subscription = nil

	if n.stanConnection != nil {
		n.stanConnection.Close()
	}
	n.stanConnection = nil

	if n.natsConnection != nil {
		n.natsConnection.Close()
	}
	n.natsConnection = nil
}

// Start creates a new subscription and executes the messageCallback on new messages
func (n *NatsBroker) Start(messageCallback stan.MsgHandler) {
	if n.subscription != nil {
		return
	}

	sub, err := n.stanConnection.QueueSubscribe(
		n.config.BrokerSubject,
		n.config.BrokerQueueGroup,
		messageCallback,
		stan.DurableName(n.config.BrokerDurableGroup),
		stan.SetManualAckMode(),
		stan.AckWait(config.New().WorkloadResponseTimeout),
	)

	_ = sub

	if err != nil {
		log.Fatal("Could not subscribe to subject")
	}

	n.subscription = sub
}

// Stop closes the natsshim subscription so no new messages will be recieved
func (n *NatsBroker) Stop() {
	n.subscription.Close()
	n.subscription = nil
}

// PublishResult result will publish the worker result to the message queue
func (n *NatsBroker) PublishResult(config config.Config, event event.Event) error {
	if config.BrokerResultSubject != "" {

		encodedData := event.DataEncoded
		err := n.stanConnection.Publish(event.Context.GetType(), encodedData)
		if err != nil {
			log.Println("Could not Publish result: ", string(encodedData))
			return errors.New("Could not Publish result: " + string(encodedData))
		}
	}
	return nil
}
