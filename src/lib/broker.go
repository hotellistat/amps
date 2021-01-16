package broker

import (
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/stan.go"
)

// NATS represents the primary NATS communication instance
type NATS struct {
	NatsConnection *nats.Conn
	StanConnection stan.Conn
	Subscription   stan.Subscription
}

// Initialize creates a new NATS connection
func Initialize() NATS {
	nc, err := nats.Connect(os.Getenv("NATS_HOST"))
	if err != nil {
		log.Fatal("Could not connect to NATS")
	}

	workerID, _ := uuid.NewRandom()

	log.Println("Worker ID:", workerID)

	sc, err := stan.Connect(os.Getenv("NATS_CLUSTER"), os.Getenv("WORKER_ID"), stan.NatsConn(nc), stan.SetConnectionLostHandler(func(_ stan.Conn, reason error) {
		log.Fatalf("Connection lost, reason: %v", reason)
	}))

	if err != nil {
		log.Fatal("Couldn't connect to NATS Streaming")
		log.Fatal(err)
	}

	return NATS{
		NatsConnection: nc,
		StanConnection: sc,
	}
}

// Teardown the NATS connection and all NATS services
func (n *NATS) Teardown() {
	if n.Subscription != nil {
		n.Subscription.Close()
	}
	n.Subscription = nil

	if n.StanConnection != nil {
		n.StanConnection.Close()
	}
	n.StanConnection = nil

	if n.NatsConnection != nil {
		n.NatsConnection.Close()
	}
	n.NatsConnection = nil
}

// Stop closes the NATS Subscription so no new messages will be recieved
func (n *NATS) Stop() {
	n.Subscription.Close()
	n.Subscription = nil
}

// Start creates a new Subscription and executes the messageCallback on new messages
func (n *NATS) Start(messageCallback stan.MsgHandler) {
	if n.Subscription != nil {
		return
	}

	aw, _ := time.ParseDuration("60s")

	sub, err := n.StanConnection.QueueSubscribe(
		os.Getenv("SUBJECT"),
		os.Getenv("QUEUE_GROUP"),
		messageCallback,
		stan.DurableName(os.Getenv("DURABLE_WORKER_GROUP")),
		stan.SetManualAckMode(),
		stan.AckWait(aw),
	)

	_ = sub

	if err != nil {
		log.Fatal("Could not subscribe to subject")
	}

	n.Subscription = sub
}
