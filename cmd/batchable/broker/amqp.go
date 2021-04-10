package broker

import (
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"log"
	"time"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/streadway/amqp"
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
			log.Printf("Received a message: %s", d.Body)
			dur, _ := time.ParseDuration("2s")
			time.Sleep(dur)
			log.Printf("Done")
			d.Ack(false)
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

	return nil
}

// Healthy checks the health of the broker
func (broker *AMQPBroker) Healthy() bool {

	return true
}
