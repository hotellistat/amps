package broker

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/getsentry/sentry-go"
	"github.com/hotellistat/AMPS/cmd/amps/cloudevent"
	"github.com/hotellistat/AMPS/cmd/amps/config"
	"github.com/hotellistat/AMPS/cmd/amps/job"
	"github.com/hotellistat/AMPS/cmd/amps/workload"
	"github.com/streadway/amqp"
	"k8s.io/apimachinery/pkg/util/json"
)

// AMQPBroker represents the primary natsshim communication instance
type AMQPBroker struct {
	running        bool
	connected      bool
	busy           *sync.Mutex
	jobManifest    *job.Manifest
	config         config.Config
	connection     *amqp.Connection
	reconnectChan  chan *amqp.Error
	consumeChannel *amqp.Channel
	publishChannel *amqp.Channel
}

// This functions retries a server connections infinitely, until it managed
// to establish a connection to the server.
func (broker *AMQPBroker) amqpConnect(uri string, errorChan chan error, localHub *sentry.Hub) *amqp.Connection {
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnect")
	})

	for {
		conn, err := amqp.Dial(uri)

		if err == nil {
			broker.connected = true
			return conn
		}

		localHub.CaptureException(err)
		println("[AMPS] dial exception", err.Error())
		errorChan <- err
		time.Sleep(1 * time.Second)
	}
}

// This routinge will always run in the background as a groutine and will
// initiate a reconnect as soon as a new event gets pushed into the
// connecitonCloseChan channel
func (broker *AMQPBroker) amqpConnectRoutine(uri string, connected chan bool) {
	localHub := sentry.CurrentHub().Clone()
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnect")
	})

	// Loop through each element in the channel (will be run upon new cahnel event)
	for range broker.reconnectChan {
		broker.running = false
		broker.connected = false
		broker.consumeChannel = nil
		broker.publishChannel = nil
		broker.busy = &sync.Mutex{}

		connectErrorChan := make(chan error)

		println("[AMPS] connecting to", uri)
		broker.connection = broker.amqpConnect(uri, connectErrorChan, localHub)

		println("[AMPS] initialized AMQP connection")

		// Register a NotifyClose on the reconnectChan channel, such that
		// a broker connection will trigger a reconnect by running a new iteration
		// of the current for loop
		broker.connection.NotifyClose(broker.reconnectChan)

		consumeChannel, consumeErr := broker.connection.Channel()

		if consumeErr != nil {
			localHub.CaptureException(consumeErr)
			println("[AMPS] could not create consumer channel")
			os.Exit(1)
		}

		qosErr := consumeChannel.Qos(1, 0, false)
		if qosErr != nil {
			localHub.CaptureException(qosErr)
		}

		_, queueErr := consumeChannel.QueueDeclare(
			broker.config.BrokerSubject,
			true,
			false,
			false,
			false,
			nil,
		)

		if queueErr != nil {
			localHub.CaptureException(queueErr)
		}

		publishChannel, publishErr := broker.connection.Channel()

		if publishErr != nil {
			localHub.CaptureException(publishErr)
			println("[AMPS] could not create publisher channel")
			os.Exit(1)
		}

		broker.consumeChannel = consumeChannel
		broker.publishChannel = publishChannel

		startErr := broker.Start()
		if startErr != nil {
			localHub.CaptureException(startErr)
		}

		// Notify the connectedChan channel, that a connection has been successfully established
		connected <- true
	}
}

// Initialize creates a new natsshim connection
func (broker *AMQPBroker) Initialize(config config.Config, jobManifest *job.Manifest) bool {
	broker.config = config
	broker.jobManifest = jobManifest
	uri := config.BrokerDsn

	// Create a new reconnectChan
	broker.reconnectChan = make(chan *amqp.Error)

	connectedChan := make(chan bool, 1)

	go broker.amqpConnectRoutine(uri, connectedChan)

	// Send initial ErrClosed event into reconnectChan cahnnel, such that
	// the AmqpConnectRoutine goroutine triggers a initial connect to the server
	broker.reconnectChan <- amqp.ErrClosed

	// Wait for initial connection confirmation in the connectedChan channel
	return <-connectedChan
}

func (broker *AMQPBroker) Evacuate() {

	broker.jobManifest.Mutex.RLock()
	defer broker.jobManifest.Mutex.RUnlock()

	println("[AMPS] Starting evacuation")
	for ID, job := range broker.jobManifest.Jobs {
		jobData := job.Message.GetData()

		println("[AMPS] evacuating job", ID, jobData)
		err := broker.publishChannel.Publish(
			"",
			broker.config.BrokerSubject,
			false,
			false,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        jobData,
			},
		)

		if err != nil {
			fmt.Println(err)
			sentry.CaptureException(err)
		}
	}

	println("[AMPS] evacuated jobs")
}

// Teardown the natsshim connection and all natsshim services
func (broker *AMQPBroker) Teardown() {
	println("[AMPS] tearing down broker")
	cancelErr := broker.consumeChannel.Cancel(broker.config.WorkerID, false)
	if cancelErr != nil {
		fmt.Println(cancelErr)
		sentry.CaptureException(cancelErr)
	}

	closeErr := broker.connection.Close()
	if closeErr != nil {
		fmt.Println(closeErr)
		sentry.CaptureException(closeErr)
	}
}

func (broker *AMQPBroker) IsRunning() bool {
	return broker.running
}

type AmqpMessageWrapper struct {
	message amqp.Delivery
}

func (wrapper AmqpMessageWrapper) GetData() []byte {
	return wrapper.message.Body
}

func (broker *AMQPBroker) messageHandler(msg amqp.Delivery) error {

	event, err := cloudevent.Unmarshal(msg.Body)
	if err != nil {
		msg.Nack(false, false)
		return err
	}

	eventID := event.Context.GetID()

	if broker.config.Debug {
		println("[AMPS] job ID:", eventID)
	}

	broker.jobManifest.Mutex.Lock()

	if broker.jobManifest.HasJob(eventID) {
		broker.jobManifest.Mutex.Unlock()
		return errors.New("[AMPS] Job ID: " + eventID + "already exists")
	}

	// Insert new into queue
	broker.jobManifest.InsertJob(
		eventID,
		AmqpMessageWrapper{
			msg,
		},
	)

	// Stop broker when the job manifest size reaches max concurrency
	stopBroker := broker.jobManifest.Size() >= broker.config.MaxConcurrency
	if stopBroker {
		stopError := (*broker).Stop()
		if stopError != nil {
			println(stopError.Error())
		}

		if broker.config.Debug {
			println("[AMPS] Max job concurrency reached, stopping broker")
		}
	}
	broker.jobManifest.Mutex.Unlock()

	// Trigger the workload endpoint by sending the job via POST
	workloadErr := workload.Trigger(event, broker.config)
	if workloadErr != nil {
		println("[AMPS]", workloadErr.Error())
		println("[AMPS] Rejecting job for rescheduling")

		broker.jobManifest.Mutex.Lock()
		if !broker.jobManifest.HasJob(eventID) {
			broker.jobManifest.Mutex.Unlock()
			println("[AMPS] Job ID:", eventID, "does not exists in the manifest")
			return nil
		}

		broker.jobManifest.DeleteJob(eventID)
		broker.jobManifest.Mutex.Unlock()

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
	broker.busy.Lock()
	defer broker.busy.Unlock()

	if broker.running {
		return nil
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
		println("[AMPS] Could not start consumer", err.Error())
		return err
	}

	broker.running = true

	go func(localHub *sentry.Hub) {
		for d := range messages {
			err := broker.messageHandler(d)
			if err != nil {
				fmt.Println(err)
				localHub.CaptureException(err)
			}
		}
	}(sentry.CurrentHub().Clone())

	return nil
}

func (broker *AMQPBroker) Stop() error {
	broker.busy.Lock()
	defer broker.busy.Unlock()

	if !broker.running {
		return nil
	}

	err := broker.consumeChannel.Cancel(broker.config.WorkerID, false)

	if err != nil {
		println("[AMPS] Could not cancel consumer", err.Error())
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
		println("[AMPS] Could not Publish result", err.Error(), string(encodedData))
		return err
	}
	return nil
}

// Healthy checks the health of the broker
func (broker *AMQPBroker) Healthy() bool {
	return broker.connected
}
