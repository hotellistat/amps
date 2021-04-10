package broker

import (
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"batchable/cmd/batchable/workload"
	"errors"
	"log"

	"github.com/cloudevents/sdk-go/v2/event"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/stan.go"
	"k8s.io/apimachinery/pkg/util/json"
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

	println(config.BrokerHost, config.BrokerCluster)

	nc, err := nats.Connect(config.BrokerHost)
	if err != nil {
		log.Fatal("Could not connect to NATS")
	}

	println("Worker ID:", config.WorkerID)

	sc, err := stan.Connect(config.BrokerCluster, config.WorkerID, stan.NatsConn(nc), stan.SetConnectionLostHandler(func(_ stan.Conn, reason error) {
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

// messageHandler will execute on every new borker message
func (broker *NatsBroker) messageHandler(
	msg *stan.Msg,
	conf *config.Config,
	jobManifest *job.Manifest) {

	event, err := cloudevent.Unmarshal(msg.Data)

	if err != nil {
		println(err.Error())
		return
	}

	eventID := event.Context.GetID()

	if conf.Debug {
		println("Job ID:", eventID)
	}

	// FlagStropBroker represents a flag that is set, so that a condition outside of our lock can evaluate
	// if the broker should be stopped. This is important, because we want to acknowledge the message before
	// the subscription is stopped, otherwise the broker may want to resend the message becaus a ack could not
	// be sent on a closed connection anymore. And because we want our mutex to be as performant as possible,
	// we want to execute the Acknowledgement outside of the mutext since the ack is blocking/sychronous.
	flagStopBroker := false

	jobManifest.Lock()

	insertErr := jobManifest.InsertJob(eventID, msg)

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

	if flagStopBroker {
		(*broker).Stop()
	}

	workloadErr := workload.Trigger(event, *conf)

	if workloadErr != nil {
		println(workloadErr.Error())
	}
}

// Start creates a new subscription and executes the messageCallback on new messages
func (n *NatsBroker) Start(jobManifest *job.Manifest) {
	if n.subscription != nil {
		return
	}

	sub, err := n.stanConnection.QueueSubscribe(
		n.config.BrokerSubject,
		n.config.BrokerQueueGroup,
		func(msg *stan.Msg) {
			n.messageHandler(msg, &n.config, jobManifest)
		},
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
	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		log.Panicln("Could not marshal cloudevent while publishing")
	}
	err := n.stanConnection.Publish(event.Context.GetType(), encodedData)
	if err != nil {
		println("Could not Publish result: ", string(encodedData))
		return errors.New("Could not Publish result: " + string(encodedData))
	}
	return nil
}

// Healthy checks the health of the broker
func (n *NatsBroker) Healthy() bool {

	natsStatus := n.natsConnection.IsConnected()

	return !natsStatus
}
