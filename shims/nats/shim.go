package main

import (
	"log"
	"time"

	"batchable/config"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/stan.go"
)

// natsshim represents the primary natsshim communication instance
type natsshim struct {
	config         config.Config
	natsConnection *nats.Conn
	stanConnection stan.Conn
	subscription   stan.Subscription
}

// Initialize creates a new natsshim connection
func (n *natsshim) Initialize(config config.Config) {
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
func (n *natsshim) Teardown() {
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
func (n *natsshim) Start(messageCallback stan.MsgHandler) {
	if n.subscription != nil {
		return
	}

	aw, _ := time.ParseDuration("60s")

	sub, err := n.stanConnection.QueueSubscribe(
		n.config.BrokerSubject,
		n.config.BrokerQueueGroup,
		messageCallback,
		stan.DurableName(n.config.BrokerDurableGroup),
		stan.SetManualAckMode(),
		stan.AckWait(aw),
	)

	_ = sub

	if err != nil {
		log.Fatal("Could not subscribe to subject")
	}

	n.subscription = sub
}

// Stop closes the natsshim subscription so no new messages will be recieved
func (n *natsshim) Stop() {
	n.subscription.Close()
	n.subscription = nil
}

// BrokerShimExport is our collective broker name
var BrokerShimExport natsshim
