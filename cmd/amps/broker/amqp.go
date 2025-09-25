package broker

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/getsentry/sentry-go"
	"github.com/hotellistat/amps/cmd/amps/cloudevent"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
	"github.com/hotellistat/amps/cmd/amps/workload"
	"github.com/streadway/amqp"
	"k8s.io/apimachinery/pkg/util/json"
)

// AMQPBroker represents the primary AMQP communication instance
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
	connMutex      *sync.RWMutex
	shutdownChan   chan bool
	isShuttingDown bool
}

// This function retries a server connection infinitely with exponential backoff,
// until it manages to establish a connection to the server.
func (broker *AMQPBroker) amqpConnect(uri string, errorChan chan error, localHub *sentry.Hub) *amqp.Connection {
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnect")
	})

	attempt := 0
	maxBackoff := 60 * time.Second
	baseBackoff := 1 * time.Second

	for {
		if broker.isShuttingDown {
			return nil
		}

		conn, err := amqp.Dial(uri)
		if err == nil {
			broker.connMutex.Lock()
			broker.connected = true
			broker.connMutex.Unlock()
			println("[AMPS] successfully connected to AMQP broker")
			return conn
		}

		attempt++
		backoff := time.Duration(math.Min(float64(baseBackoff)*math.Pow(2, float64(attempt)), float64(maxBackoff)))

		localHub.CaptureException(err)
		println("[AMPS] dial exception:", err.Error(), "- retrying in", backoff)
		errorChan <- err

		select {
		case <-time.After(backoff):
			// Continue to retry
		case <-broker.shutdownChan:
			return nil
		}
	}
}

// This function creates channels with retry logic and sets up QoS-based concurrency control
func (broker *AMQPBroker) createChannels(localHub *sentry.Hub) error {
	maxRetries := 5
	baseDelay := 1 * time.Second

	for retry := 0; retry < maxRetries; retry++ {
		if broker.isShuttingDown {
			return errors.New("shutting down")
		}

		broker.connMutex.RLock()
		conn := broker.connection
		broker.connMutex.RUnlock()

		if conn == nil || conn.IsClosed() {
			return errors.New("connection is nil or closed")
		}

		consumeChannel, consumeErr := conn.Channel()
		if consumeErr != nil {
			localHub.CaptureException(consumeErr)
			println("[AMPS] could not create consumer channel, attempt", retry+1, ":", consumeErr.Error())
			time.Sleep(baseDelay * time.Duration(retry+1))
			continue
		}

		// Set QoS prefetch count to MaxConcurrency. This replaces the previous manual
		// start/stop broker logic. RabbitMQ will only deliver up to MaxConcurrency
		// unacknowledged messages to this consumer, providing automatic concurrency control.
		qosErr := consumeChannel.Qos(broker.config.MaxConcurrency, 0, false)
		if qosErr != nil {
			localHub.CaptureException(qosErr)
			consumeChannel.Close()
			println("[AMPS] could not set QoS, attempt", retry+1, ":", qosErr.Error())
			time.Sleep(baseDelay * time.Duration(retry+1))
			continue
		}

		publishChannel, publishErr := conn.Channel()
		if publishErr != nil {
			localHub.CaptureException(publishErr)
			consumeChannel.Close()
			println("[AMPS] could not create publisher channel, attempt", retry+1, ":", publishErr.Error())
			time.Sleep(baseDelay * time.Duration(retry+1))
			continue
		}

		broker.connMutex.Lock()
		broker.consumeChannel = consumeChannel
		broker.publishChannel = publishChannel
		broker.connMutex.Unlock()

		println("[AMPS] successfully created AMQP channels")
		return nil
	}

	return errors.New("failed to create channels after all retries")
}

// This routine will always run in the background as a goroutine and will
// initiate a reconnect as soon as a new event gets pushed into the
// reconnectChan channel
func (broker *AMQPBroker) amqpConnectRoutine(uri string, connected chan bool) {
	localHub := sentry.CurrentHub().Clone()
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnectRoutine")
	})

	// Loop through each element in the channel (will be run upon new channel event)
	for range broker.reconnectChan {
		if broker.isShuttingDown {
			return
		}

		broker.connMutex.Lock()
		broker.running = false
		broker.connected = false
		if broker.consumeChannel != nil {
			broker.consumeChannel.Close()
			broker.consumeChannel = nil
		}
		if broker.publishChannel != nil {
			broker.publishChannel.Close()
			broker.publishChannel = nil
		}
		if broker.connection != nil {
			broker.connection.Close()
			broker.connection = nil
		}
		broker.busy = &sync.Mutex{}
		broker.connMutex.Unlock()

		connectErrorChan := make(chan error)

		println("[AMPS] connecting to", uri)
		conn := broker.amqpConnect(uri, connectErrorChan, localHub)
		if conn == nil {
			// Shutdown was requested
			return
		}

		broker.connMutex.Lock()
		broker.connection = conn
		broker.connMutex.Unlock()

		// Register a NotifyClose on the reconnectChan channel, such that
		// a broker connection close will trigger a reconnect by running a new iteration
		// of the current for loop
		conn.NotifyClose(broker.reconnectChan)

		// Create channels with retry logic
		err := broker.createChannels(localHub)
		if err != nil {
			localHub.CaptureException(err)
			println("[AMPS] failed to create channels:", err.Error())
			// Trigger reconnection
			broker.reconnectChan <- amqp.ErrClosed
			continue
		}

		startErr := broker.Start()
		if startErr != nil {
			localHub.CaptureException(startErr)
			println("[AMPS] failed to start consumer:", startErr.Error())
			// Trigger reconnection
			broker.reconnectChan <- amqp.ErrClosed
			continue
		}

		// Notify the connectedChan channel, that a connection has been successfully established
		select {
		case connected <- true:
		default:
			// Channel might be full or closed, that's okay
		}
	}
}

// Initialize creates a new AMQP connection
func (broker *AMQPBroker) Initialize(config config.Config, jobManifest *job.Manifest) bool {
	broker.config = config
	broker.jobManifest = jobManifest
	broker.connMutex = &sync.RWMutex{}
	broker.shutdownChan = make(chan bool, 1)
	broker.isShuttingDown = false
	uri := config.BrokerDsn

	// Create a new reconnectChan
	broker.reconnectChan = make(chan *amqp.Error, 10) // Buffer to prevent blocking

	connectedChan := make(chan bool, 1)

	go broker.amqpConnectRoutine(uri, connectedChan)

	// Send initial ErrClosed event into reconnectChan channel, such that
	// the AmqpConnectRoutine goroutine triggers an initial connect to the server
	broker.reconnectChan <- amqp.ErrClosed

	// Wait for initial connection confirmation in the connectedChan channel
	// with timeout to prevent indefinite blocking
	select {
	case success := <-connectedChan:
		return success
	case <-time.After(30 * time.Second):
		println("[AMPS] timeout waiting for initial connection")
		return false
	}
}

func (broker *AMQPBroker) Evacuate() {
	broker.jobManifest.Mutex.RLock()
	defer broker.jobManifest.Mutex.RUnlock()

	println("[AMPS] Starting evacuation")
	for ID, job := range broker.jobManifest.Jobs {
		println("[AMPS] evacuating job", ID)

		// Nack the message to return it to the queue instead of re-publishing
		// This is more efficient and preserves message ordering
		if job.Delivery != nil {
			nackErr := job.Delivery.Nack(false, true)
			if nackErr != nil {
				fmt.Println("[AMPS] failed to nack job during evacuation", ID, ":", nackErr.Error())
				sentry.CaptureException(nackErr)
			}
		}
	}

	println("[AMPS] evacuation completed")
}

// publishWithRetry attempts to publish a message with retries
func (broker *AMQPBroker) publishWithRetry(routingKey string, body []byte, maxRetries int) error {
	for attempt := 0; attempt < maxRetries; attempt++ {
		broker.connMutex.RLock()
		publishChannel := broker.publishChannel
		connected := broker.connected
		broker.connMutex.RUnlock()

		if !connected || publishChannel == nil {
			if attempt < maxRetries-1 {
				println("[AMPS] not connected, waiting before retry attempt", attempt+1)
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return errors.New("not connected to AMQP broker")
		}

		err := publishChannel.Publish(
			"",
			routingKey,
			false,
			false,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        body,
			},
		)

		if err == nil {
			return nil // Success
		}

		println("[AMPS] publish failed, attempt", attempt+1, ":", err.Error())
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}

	return errors.New("failed to publish after all retries")
}

// Teardown the AMQP connection and all AMQP services
func (broker *AMQPBroker) Teardown() {
	println("[AMPS] tearing down broker")
	broker.isShuttingDown = true

	// Signal shutdown
	select {
	case broker.shutdownChan <- true:
	default:
	}

	broker.connMutex.Lock()
	defer broker.connMutex.Unlock()

	if broker.consumeChannel != nil {
		cancelErr := broker.consumeChannel.Cancel(broker.config.WorkerID, false)
		if cancelErr != nil {
			fmt.Println("[AMPS] error canceling consumer:", cancelErr.Error())
			sentry.CaptureException(cancelErr)
		}
		broker.consumeChannel.Close()
		broker.consumeChannel = nil
	}

	if broker.publishChannel != nil {
		broker.publishChannel.Close()
		broker.publishChannel = nil
	}

	if broker.connection != nil {
		closeErr := broker.connection.Close()
		if closeErr != nil {
			fmt.Println("[AMPS] error closing connection:", closeErr.Error())
			sentry.CaptureException(closeErr)
		}
		broker.connection = nil
	}

	broker.connected = false
	broker.running = false
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

func (wrapper AmqpMessageWrapper) Ack(multiple bool) error {
	return wrapper.message.Ack(multiple)
}

func (wrapper AmqpMessageWrapper) Nack(multiple, requeue bool) error {
	return wrapper.message.Nack(multiple, requeue)
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
		return errors.New("[AMPS] Job ID: " + eventID + " already exists")
	}

	// Insert new job into queue with AMQP delivery for later acknowledgment.
	// Concurrency is now controlled by RabbitMQ QoS prefetch limit rather than manually stopping/starting the consumer.
	messageWrapper := AmqpMessageWrapper{msg}
	broker.jobManifest.InsertJobWithDelivery(
		eventID,
		messageWrapper,
		messageWrapper,
	)
	broker.jobManifest.Mutex.Unlock()

	// Trigger the workload endpoint by sending the job via POST
	workloadErr := workload.Trigger(event, broker.config)
	if workloadErr != nil {
		println("[AMPS]", workloadErr.Error())
		println("[AMPS] Rejecting job for rescheduling")

		broker.jobManifest.Mutex.Lock()
		if !broker.jobManifest.HasJob(eventID) {
			broker.jobManifest.Mutex.Unlock()
			println("[AMPS] Job ID:", eventID, "does not exist in the manifest")
			return nil
		}

		broker.jobManifest.DeleteJob(eventID)
		broker.jobManifest.Mutex.Unlock()

		// Negative acknowledge and reschedule job for a different worker to handle
		// since the workload on this instance seems to not be working
		nackErr := msg.Nack(false, true)
		if nackErr != nil {
			println("[AMPS] error nacking message:", nackErr.Error())
		}
	}
	// Note: We no longer acknowledge the message here. The message will be acknowledged
	// only when the workload calls /acknowledge or /reject endpoints, ensuring proper
	// message reliability and preventing message loss if the workload fails.

	return nil
}

// Start creates a new subscription and executes the messageCallback on new messages
func (broker *AMQPBroker) Start() error {
	if broker.busy == nil {
		broker.busy = &sync.Mutex{}
	}

	broker.busy.Lock()
	defer broker.busy.Unlock()

	if broker.running {
		return nil
	}

	broker.connMutex.RLock()
	consumeChannel := broker.consumeChannel
	connected := broker.connected
	broker.connMutex.RUnlock()

	if !connected || consumeChannel == nil {
		return errors.New("not connected or channel not available")
	}

	messages, err := consumeChannel.Consume(
		broker.config.BrokerSubject,
		broker.config.WorkerID,
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		println("[AMPS] Could not start consumer:", err.Error())
		return err
	}

	broker.running = true

	go func(localHub *sentry.Hub) {
		localHub.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("goroutine", "messageConsumer")
		})

		for d := range messages {
			if broker.isShuttingDown {
				break
			}

			err := broker.messageHandler(d)
			if err != nil {
				fmt.Println("[AMPS] message handler error:", err.Error())
				localHub.CaptureException(err)
			}
		}
		println("[AMPS] message consumer goroutine ended")
	}(sentry.CurrentHub().Clone())

	return nil
}

func (broker *AMQPBroker) Stop() error {
	if broker.busy == nil {
		return nil
	}

	broker.busy.Lock()
	defer broker.busy.Unlock()

	if !broker.running {
		return nil
	}

	broker.connMutex.RLock()
	consumeChannel := broker.consumeChannel
	broker.connMutex.RUnlock()

	if consumeChannel != nil {
		err := consumeChannel.Cancel(broker.config.WorkerID, false)
		if err != nil {
			println("[AMPS] Could not cancel consumer:", err.Error())
			return err
		}
	}

	broker.running = false
	return nil
}

// PublishMessage publishes a message to the queue with retry logic
func (broker *AMQPBroker) PublishMessage(event event.Event) error {
	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		return errors.New("could not marshal cloudevent while publishing")
	}

	err := broker.publishWithRetry(event.Context.GetType(), encodedData, 3)
	if err != nil {
		println("[AMPS] Could not publish result:", err.Error())
		return err
	}

	return nil
}

// Healthy checks the health of the broker
func (broker *AMQPBroker) Healthy() bool {
	broker.connMutex.RLock()
	connected := broker.connected
	connection := broker.connection
	broker.connMutex.RUnlock()

	return connected && connection != nil && !connection.IsClosed()
}
