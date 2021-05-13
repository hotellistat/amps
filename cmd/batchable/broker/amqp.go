package broker

import (
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"batchable/cmd/batchable/workload"
	"errors"
	"log"
	"sync"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/streadway/amqp"
	"k8s.io/apimachinery/pkg/util/json"
)

// AMQPBroker represents the primary natsshim communication instance
type AMQPBroker struct {
	running        bool
	jobManifest    *job.Manifest
	config         config.Config
	connection     *amqp.Connection
	consumeChannel *amqp.Channel
	publishChannel *amqp.Channel
	mutex          *sync.Mutex
}

// Initialize creates a new natsshim connection
func (broker *AMQPBroker) Initialize(config config.Config, jobManifest *job.Manifest) {
	broker.mutex = &sync.Mutex{}
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

	consumeChannel, consumeErr := broker.connection.Channel()

	if consumeErr != nil {
		log.Fatal("Could not create consumer channel")
	}

	consumeChannel.Qos(1, 0, false)

	exchangeErr := consumeChannel.ExchangeDeclare(broker.config.BrokerSubject, "fanout", true, false, false, false, nil)

	if exchangeErr != nil {
		log.Fatal("Could not declare exchange: ", exchangeErr.Error())
	}

	broker.consumeChannel = consumeChannel

	publishChannel, publishErr := broker.connection.Channel()

	if publishErr != nil {
		log.Fatal("Could not create publisher channel")
	}

	broker.publishChannel = publishChannel

	println("[batchable] Initialized AMQP connection")
}

// Teardown the natsshim connection and all natsshim services
func (broker *AMQPBroker) Teardown() {
	println("[batchable] Tearing down broker")
	broker.consumeChannel.Cancel(broker.config.WorkerID, false)
	broker.connection.Close()
}

func (broker *AMQPBroker) Running() bool {
	return broker.running
}

type AmqpMessageWrapper struct {
	message amqp.Delivery
}

func (broker *AMQPBroker) messageHandler(msg amqp.Delivery) error {

	event, err := cloudevent.Unmarshal(msg.Body)
	if err != nil {
		return err
	}

	eventID := event.Context.GetID()

	if broker.config.Debug {
		println("[batchable] Job ID:", eventID)
	}

	broker.jobManifest.Mutex.Lock()
	defer broker.jobManifest.Mutex.Unlock()

	// Insert new into queue
	insertErr := broker.jobManifest.InsertJob(
		eventID,
		AmqpMessageWrapper{
			msg,
		},
	)
	if insertErr != nil {
		return insertErr
	}

	// Stop broker when the job manifest size reaches max concurrency
	stopBroker := broker.jobManifest.Size() >= broker.config.MaxConcurrency
	if stopBroker {
		stopError := (*broker).Stop()
		if stopError != nil {
			println(stopError.Error())
		}

		if broker.config.Debug {
			println("[batchable] Max job concurrency reached, stopping broker")
		}
	}

	// Trigger the workload endpoint by sending the job via POST
	workloadErr := workload.Trigger(event, broker.config)
	if workloadErr != nil {
		println("[batchable]", workloadErr.Error())
		println("[batchable] Rejecting job for rescheduling")
		delError := broker.jobManifest.DeleteJob(eventID)
		if delError != nil {
			println(delError.Error())
		}

		// Negative acknowlege and reschedule job for a different worker to handle
		// since the workload on this instance seems to not be working
		nackErr := msg.Nack(false, true)
		if nackErr != nil {
			println(nackErr.Error())
		}
	} else {
		// Acknowledge the job. This will trigger a new message delivery since the
		// channle "Qos" is set to "1", only allowing one inflight message at a time
		ackErr := msg.Ack(false)
		if ackErr != nil {
			println(ackErr.Error())
		}
	}

	return nil
}

// Start creates a new subscription and executes the messageCallback on new messages
func (broker *AMQPBroker) Start() error {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()

	if broker.running {
		return errors.New("broker is already running")
	}

	messages, err := broker.consumeChannel.Consume(
		broker.config.BrokerSubject,
		broker.config.WorkerID,
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Println("[batchable] Could not start consumer", err.Error())
		return err
	}

	broker.running = true

	go func() {
		for d := range messages {
			err := broker.messageHandler(d)
			if err != nil {
				println(err.Error())
			}
		}
	}()

	return nil
}

func (broker *AMQPBroker) Stop() error {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()

	if !broker.running {
		return errors.New("broker is already stopped")
	}

	err := broker.consumeChannel.Cancel(broker.config.WorkerID, false)

	if err != nil {
		log.Println("[batchable] Could not cancel consumer", err.Error())
		return err
	}

	broker.running = false

	return nil
}

// PublishResult result will publish the worker result to the message queue
func (broker *AMQPBroker) PublishMessage(event event.Event) error {
	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		log.Panicln("Could not marshal cloudevent while publishing")
	}

	err := broker.publishChannel.Publish(
		"",
		event.Context.GetType(),
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        encodedData,
		})

	if err != nil {
		println("[batchable] Could not Publish result", err.Error())
		return errors.New("Could not Publish result: " + string(encodedData))
	}
	return nil
}

// Healthy checks the health of the broker
func (broker *AMQPBroker) Healthy() bool {
	return true
}
