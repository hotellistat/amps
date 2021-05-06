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

	ch, err := broker.connection.Channel()
	if err != nil {
		log.Fatal("Could not connect to AMQP server")
	}

	ch.Qos(broker.config.MaxConcurrency, 0, false)

	ch.QueueDeclare(
		broker.config.BrokerSubject,
		true,
		false,
		false,
		false,
		amqp.Table{
			"message-ttl": int32(broker.config.JobTimeout.Milliseconds()),
		},
	)

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

type AmqpMessageWrapper struct {
	message amqp.Delivery
}

func (messageWrapper AmqpMessageWrapper) Ack() error {
	return messageWrapper.message.Ack(false)
}

func (messageWrapper AmqpMessageWrapper) Reject() error {
	return messageWrapper.message.Nack(false, true)
}

func (broker *AMQPBroker) messageHandler(msg amqp.Delivery) error {

	event, err := cloudevent.Unmarshal(msg.Body)

	if err != nil {
		return err
	}

	eventID := event.Context.GetID()

	if broker.config.Debug {
		println("Job ID:", eventID)
	}

	messageWrapper := AmqpMessageWrapper{
		msg,
	}

	insertErr := broker.jobManifest.InsertJob(eventID, messageWrapper)

	if insertErr != nil {
		return insertErr
	}

	workloadErr := workload.Trigger(event, broker.config)

	if workloadErr != nil {
		println(workloadErr.Error())
		println("Rejecting job for rescheduling")
		broker.jobManifest.DeleteJob(eventID)
		msg.Nack(false, true)
	}

	return nil
}

// Start creates a new subscription and executes the messageCallback on new messages
func (broker *AMQPBroker) Start() error {
	if broker.running {
		return errors.New("broker is already running")
	}

	messages, err := broker.channel.Consume(
		broker.config.BrokerSubject,
		broker.config.WorkerID,
		broker.config.InstantAck,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Println("Could not consume", err.Error())
		return err
	}

	broker.running = true

	go func() {
		for d := range messages {
			err := broker.messageHandler(d)
			if err != nil {
				log.Println(err.Error())
			}
		}
	}()

	return nil
}

func (broker *AMQPBroker) Stop() error {
	return nil
}

// PublishResult result will publish the worker result to the message queue
func (broker *AMQPBroker) PublishMessage(event event.Event) error {

	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		log.Panicln("Could not marshal cloudevent while publishing")
	}

	err := broker.channel.Publish(
		"",
		event.Context.GetType(),
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        encodedData,
		})

	if err != nil {
		println("Could not Publish result", err.Error())
		return errors.New("Could not Publish result: " + string(encodedData))
	}
	return nil
}

// Healthy checks the health of the broker
func (broker *AMQPBroker) Healthy() bool {

	return true
}
