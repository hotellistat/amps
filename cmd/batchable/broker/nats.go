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
	running        bool
	jobManifest    *job.Manifest
	config         config.Config
	natsConnection *nats.Conn
	stanConnection stan.Conn
	subscription   stan.Subscription
}

// Initialize creates a new natsshim connection
func (broker *NatsBroker) Initialize(config config.Config, jobManifest *job.Manifest) {
	broker.config = config
	broker.jobManifest = jobManifest

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

	broker.natsConnection = nc
	broker.stanConnection = sc
}

func (broker *NatsBroker) Running() bool {
	return broker.running
}

// Teardown the natsshim connection and all natsshim services
func (broker *NatsBroker) Teardown() {
	if broker.subscription != nil {
		broker.subscription.Close()
	}
	broker.subscription = nil

	if broker.stanConnection != nil {
		broker.stanConnection.Close()
	}
	broker.stanConnection = nil

	if broker.natsConnection != nil {
		broker.natsConnection.Close()
	}
	broker.natsConnection = nil
}

type NatsMessageWrapper struct {
	message *stan.Msg
}

func (messageWrapper NatsMessageWrapper) Ack() error {
	return messageWrapper.message.Ack()
}

func (messageWrapper NatsMessageWrapper) Reject() error {
	return nil
}

// messageHandler will execute on every new borker message
func (broker *NatsBroker) messageHandler(msg *stan.Msg) {

	event, err := cloudevent.Unmarshal(msg.Data)

	if err != nil {
		println(err.Error())
		return
	}

	eventID := event.Context.GetID()

	if broker.config.Debug {
		println("Job ID:", eventID)
	}

	// FlagStropBroker represents a flag that is set, so that a condition outside of our lock can evaluate
	// if the broker should be stopped. This is important, because we want to acknowledge the message before
	// the subscription is stopped, otherwise the broker may want to resend the message becaus a ack could not
	// be sent on a closed connection anymore. And because we want our mutex to be as performant as possible,
	// we want to execute the Acknowledgement outside of the mutext since the ack is blocking/sychronous.
	flagStopBroker := false

	broker.jobManifest.Lock()

	messageWrapper := NatsMessageWrapper{
		msg,
	}

	insertErr := broker.jobManifest.InsertJob(eventID, messageWrapper)

	if insertErr != nil {
		println(insertErr.Error())
		return
	}

	if broker.jobManifest.Size() >= broker.config.MaxConcurrency {
		if broker.config.Debug {
			println("Max job concurrency reached, stopping broker")
		}
		flagStopBroker = true
	}

	broker.jobManifest.Unlock()

	if broker.config.InstantAck {
		msg.Ack()
	}

	if flagStopBroker {
		(*broker).Stop()
	}

	workloadErr := workload.Trigger(event, broker.config)

	if workloadErr != nil {
		println(workloadErr.Error())
	}
}

// Start creates a new subscription and executes the messageCallback on new messages
func (broker *NatsBroker) Start() error {
	if broker.subscription != nil {
		return errors.New("queue subscription without broker connection not possible")
	}

	sub, err := broker.stanConnection.QueueSubscribe(
		broker.config.BrokerSubject,
		broker.config.BrokerSubject,
		broker.messageHandler,
		stan.DurableName(broker.config.BrokerSubject),
		stan.SetManualAckMode(),
		stan.AckWait(broker.config.JobTimeout),
	)

	if err != nil {
		log.Fatal("Could not subscribe to subject")
	}

	broker.subscription = sub
	broker.running = true
	return nil
}

// Stop closes the natsshim subscription so no new messages will be recieved
func (broker *NatsBroker) Stop() {
	broker.subscription.Close()
	broker.subscription = nil
	broker.running = false
}

// PublishResult result will publish the worker result to the message queue
func (broker *NatsBroker) PublishMessage(event event.Event) error {
	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		log.Panicln("Could not marshal cloudevent while publishing")
	}
	err := broker.stanConnection.Publish(event.Context.GetType(), encodedData)
	if err != nil {
		println("Could not Publish result: ", string(encodedData))
		return errors.New("Could not Publish result: " + string(encodedData))
	}
	return nil
}

// Healthy checks the health of the broker
func (broker *NatsBroker) Healthy() bool {

	natsStatus := broker.natsConnection.IsConnected()

	return !natsStatus
}
