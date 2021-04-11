package broker

import (
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"batchable/cmd/batchable/workload"
	"errors"
	"log"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/streadway/amqp"
	"k8s.io/apimachinery/pkg/util/json"
)

// AMQPBroker represents the primary natsshim communication instance
type AMQPBroker struct {
	running     bool
	jobManifest *job.Manifest
	config      config.Config
	connection  *amqp.Connection
	channel     *amqp.Channel
	queue       *amqp.Queue
}

// Initialize creates a new natsshim connection
func (broker *AMQPBroker) Initialize(config config.Config, jobManifest *job.Manifest) {
	broker.config = config
	broker.jobManifest = jobManifest

	auth := ""

	if config.BrokerUsername != "" && config.BrokerPassword != "" {
		auth = config.BrokerUsername + ":" + config.BrokerPassword + "@"
	}

	uri := "amqp://" + auth + config.BrokerHost + "/"

	conn, err := amqp.Dial(uri)

	if err != nil {
		log.Fatal("Could not connect to AMQP server")
	}

	broker.connection = conn

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("Could not connect to AMQP server")
	}

	queue, _ := ch.QueueDeclare(broker.config.BrokerSubject, true, false, false, false, nil)

	broker.queue = &queue

	broker.channel = ch

	println("Initialized AMQP connection")
}

// Teardown the natsshim connection and all natsshim services
func (broker *AMQPBroker) Teardown() {
	println("Tearing down broker")
	broker.channel.Cancel(broker.config.WorkerID, false)
	broker.connection.Close()
}

func (broker *AMQPBroker) Running() bool {
	return broker.running
}

type MessageWrapper struct {
	message amqp.Delivery
}

func (messageWrapper MessageWrapper) Ack() error {
	return messageWrapper.message.Ack(false)
}

func (broker *AMQPBroker) messageHandler(msg amqp.Delivery) {

	event, err := cloudevent.Unmarshal(msg.Body)

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

	messageWrapper := MessageWrapper{
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
		msg.Ack(false)
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
func (broker *AMQPBroker) Start() error {
	messages, err := broker.channel.Consume(
		broker.config.BrokerSubject,
		broker.config.WorkerID,
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Fatal("Could not consume", err.Error())
	}

	broker.running = true

	go func() {
		for d := range messages {
			broker.messageHandler(d)
		}
	}()

	return nil
}

// Stop closes the natsshim subscription so no new messages will be recieved
func (broker *AMQPBroker) Stop() {
	broker.channel.Cancel(broker.config.WorkerID, false)
	broker.running = false
}

// PublishResult result will publish the worker result to the message queue
func (broker *AMQPBroker) PublishMessage(event event.Event) error {

	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		log.Panicln("Could not marshal cloudevent while publishing")
	}

	err := broker.channel.Publish(
		"",
		broker.queue.Name,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        encodedData,
		})

	if err != nil {
		println("Could not Publish result: ", string(encodedData))
		return errors.New("Could not Publish result: " + string(encodedData))
	}
	return nil
}

// Healthy checks the health of the broker
func (broker *AMQPBroker) Healthy() bool {

	return true
}
