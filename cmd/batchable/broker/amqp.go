package broker

import (
	"batchable/cmd/batchable/cloudevent"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"batchable/cmd/batchable/workload"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/streadway/amqp"
	"k8s.io/apimachinery/pkg/util/json"
)

// AMQPBroker represents the primary natsshim communication instance
type AMQPBroker struct {
	running             bool
	jobManifest         *job.Manifest
	config              config.Config
	connection          *amqp.Connection
	connectionCloseChan chan *amqp.Error
	consumeChannel      *amqp.Channel
	publishChannel      *amqp.Channel
	mutex               *sync.Mutex
}

// This functions retries a server connections infinitely, until it managed
// to establish a connection to the server.
func (broker *AMQPBroker) AmqpConnect(uri string) *amqp.Connection {
	for {
		conn, err := amqp.Dial(uri)

		if err == nil {
			return conn
		}

		println("[batchable] Dial exception", err.Error())
		time.Sleep(1 * time.Second)
	}
}

// This routinge will always run in the background as a groutine and will
// initiate a reconnect as soon as a new event gets pushed into the
// connecitonCloseChan channel
func (broker *AMQPBroker) AmqpConnectRoutine(uri string, connected chan bool) {
	// Loop through each element in the channel (will be run upon new cahnel event)
	for range broker.connectionCloseChan {
		broker.mutex = &sync.Mutex{}
		broker.running = false

		println("[batchable] Connecting to", uri)
		broker.connection = broker.AmqpConnect(uri)
		println("[batchable] Initialized AMQP connection")

		// Register a NotifyClose on the connectionCloseChan channel, such that
		// a broker connection will trigger a reconnect by running a new iteration
		// of the current for loop
		broker.connection.NotifyClose(broker.connectionCloseChan)

		consumeChannel, consumeErr := broker.connection.Channel()

		if consumeErr != nil {
			log.Fatal("Could not create consumer channel")
		}

		consumeChannel.Qos(1, 0, false)

		consumeChannel.QueueDeclare(
			broker.config.BrokerSubject,
			true,
			false,
			false,
			false,
			nil,
		)

		publishChannel, publishErr := broker.connection.Channel()

		if publishErr != nil {
			log.Fatal("Could not create publisher channel")
		}

		broker.consumeChannel = consumeChannel
		broker.publishChannel = publishChannel

		broker.Start()

		// Notify the connectedChan channel, that a connection has been successfully established
		connected <- true
	}
}

// Initialize creates a new natsshim connection
func (broker *AMQPBroker) Initialize(config config.Config, jobManifest *job.Manifest) bool {
	broker.config = config
	broker.jobManifest = jobManifest

	auth := ""

	if config.BrokerUsername != "" && config.BrokerPassword != "" {
		auth = config.BrokerUsername + ":" + config.BrokerPassword + "@"
	}

	uri := "amqp://" + auth + config.BrokerHost + "/"

	// Create a new connectionCloseChan
	broker.connectionCloseChan = make(chan *amqp.Error)

	connectedChan := make(chan bool, 1)

	go broker.AmqpConnectRoutine(uri, connectedChan)

	// Send initial ErrClosed event into connectionCloseChan cahnnel, such that
	// the AmqpConnectRoutine goroutine triggers a initial connect to the server
	broker.connectionCloseChan <- amqp.ErrClosed

	// Wait for initial connection confirmation in the connectedChan channel
	return <-connectedChan
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
		println("[batchable] Could not start consumer", err.Error())
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
		println("[batchable] Could not cancel consumer", err.Error())
		return err
	}

	broker.running = false

	return nil
}

// PublishResult result will publish the worker result to the message queue
func (broker *AMQPBroker) PublishMessage(event event.Event) error {
	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		return errors.New("could not marshal cloudevent while publishing")
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
