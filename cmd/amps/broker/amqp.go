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
	running              bool
	connected            bool
	busy                 *sync.Mutex
	jobManifest          *job.Manifest
	config               config.Config
	connection           *amqp.Connection
	reconnectChan        chan bool
	consumeChannel       *amqp.Channel
	publishChannel       *amqp.Channel
	connMutex            *sync.RWMutex
	shutdownChan         chan bool
	isShuttingDown       bool
	lastConnected        time.Time
	reconnectCount       int
	lastHealthCheck      time.Time
	consecutiveFailures  int
	fullyInitialized     bool
	startupTime          time.Time
	lastReconnectAttempt time.Time
}

// This function retries a server connection infinitely with exponential backoff,
// until it manages to establish a connection to the server.
func (broker *AMQPBroker) amqpConnect(uri string, errorChan chan error, localHub *sentry.Hub) *amqp.Connection {
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnect")
	})

	attempt := 0
	maxBackoff := 30 * time.Second
	baseBackoff := 1 * time.Second

	for {
		if broker.isShuttingDown {
			return nil
		}

		conn, err := amqp.Dial(uri)
		if err == nil {
			broker.connMutex.Lock()
			broker.connected = true
			broker.lastConnected = time.Now()
			broker.reconnectCount++
			broker.connMutex.Unlock()
			println("[AMPS] successfully connected to AMQP broker (attempt", broker.reconnectCount, ") - establishing channels...")
			return conn
		}

		attempt++
		backoff := time.Duration(math.Min(float64(baseBackoff)*math.Pow(2, float64(attempt)), float64(maxBackoff)))

		localHub.CaptureException(err)
		println("[AMPS] dial exception:", err.Error(), "- retrying in", backoff, "(attempt", attempt, ")")

		// Don't block on errorChan if no one is reading it
		select {
		case errorChan <- err:
		default:
		}

		select {
		case <-time.After(backoff):
			println("[AMPS] retrying connection after backoff...")
		case <-broker.shutdownChan:
			println("[AMPS] connection attempts cancelled due to shutdown")
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

		println("[AMPS] successfully created AMQP channels - starting consumer...")
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

	reconnectionAttempt := 0

	// Loop indefinitely, checking for reconnection signals
	for {
		select {
		case <-broker.reconnectChan:
			reconnectionAttempt++
			broker.connMutex.Lock()
			broker.lastReconnectAttempt = time.Now()
			broker.connMutex.Unlock()
			println("[AMPS] reconnection signal received (attempt", reconnectionAttempt, ")")
		case <-broker.shutdownChan:
			println("[AMPS] reconnection routine shutting down")
			return
		}
		if broker.isShuttingDown {
			println("[AMPS] skipping reconnection - shutting down")
			return
		}

		broker.connMutex.Lock()
		broker.running = false
		broker.connected = false
		broker.fullyInitialized = false // Reset initialization state on reconnection
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

		connectErrorChan := make(chan error, 10)

		println("[AMPS] starting connection attempt to", uri)

		// Start a goroutine to consume connection errors
		go func() {
			for err := range connectErrorChan {
				if err != nil {
					// Just log the error, amqpConnect handles retries internally
					println("[AMPS] connection error:", err.Error())
				}
			}
		}()

		conn := broker.amqpConnect(uri, connectErrorChan, localHub)
		close(connectErrorChan) // Close the channel when connection attempt is done

		if conn == nil {
			println("[AMPS] connection attempt failed - shutdown was requested")
			return
		}

		broker.connMutex.Lock()
		broker.connection = conn
		broker.connMutex.Unlock()

		// Create a dedicated close notification channel for this connection
		closeNotify := make(chan *amqp.Error, 1)
		conn.NotifyClose(closeNotify)

		// Start a goroutine to monitor this specific connection's close events
		go func(connectionAttempt int) {
			closeErr := <-closeNotify
			if closeErr != nil && !broker.isShuttingDown {
				broker.connMutex.Lock()
				uptime := time.Since(broker.lastConnected)
				broker.connMutex.Unlock()

				println("[AMPS] connection", connectionAttempt, "closed after", uptime, ":", closeErr.Error())

				// Signal reconnection needed with non-blocking send
				select {
				case broker.reconnectChan <- true:
					println("[AMPS] reconnection signal sent successfully")
				default:
					println("[AMPS] reconnection channel full - forcing signal")
					// Force drain one item and add our signal
					select {
					case <-broker.reconnectChan:
					default:
					}
					broker.reconnectChan <- true
				}
			} else if closeErr == nil {
				println("[AMPS] connection", connectionAttempt, "closed gracefully")
			}
		}(reconnectionAttempt)

		// Create channels with retry logic
		err := broker.createChannels(localHub)
		if err != nil {
			localHub.CaptureException(err)
			println("[AMPS] failed to create channels:", err.Error(), "- will retry")
			// Trigger reconnection after a short delay
			go func() {
				time.Sleep(2 * time.Second)
				select {
				case broker.reconnectChan <- true:
					println("[AMPS] reconnection scheduled after channel creation failure")
				default:
					println("[AMPS] reconnection already queued after channel failure")
				}
			}()
			continue
		}

		startErr := broker.Start()
		if startErr != nil {
			localHub.CaptureException(startErr)
			println("[AMPS] failed to start consumer:", startErr.Error(), "- will retry")
			// Trigger reconnection after a short delay
			go func() {
				time.Sleep(2 * time.Second)
				select {
				case broker.reconnectChan <- true:
					println("[AMPS] reconnection scheduled after consumer start failure")
				default:
					println("[AMPS] reconnection already queued after consumer failure")
				}
			}()
			continue
		}

		// Mark broker as fully initialized after successful start
		broker.connMutex.Lock()
		broker.fullyInitialized = true
		broker.connMutex.Unlock()
		println("[AMPS] broker fully initialized and ready to process messages")

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
	broker.fullyInitialized = false
	broker.startupTime = time.Now()
	uri := config.BrokerDsn

	// Create a new reconnectChan with larger buffer to prevent blocking
	broker.reconnectChan = make(chan bool, 20)

	connectedChan := make(chan bool, 1)

	go broker.amqpConnectRoutine(uri, connectedChan)

	// Send initial reconnection signal to trigger initial connect to the server
	broker.reconnectChan <- true

	// Start periodic health monitoring
	go broker.startHealthMonitor()

	// Start reconnection watchdog
	go broker.startReconnectionWatchdog()

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

	// Signal shutdown to all goroutines
	select {
	case broker.shutdownChan <- true:
	default:
	}

	// Give goroutines a moment to process shutdown signal
	time.Sleep(100 * time.Millisecond)

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

	// Close reconnection channel to stop the reconnection goroutine
	if broker.reconnectChan != nil {
		close(broker.reconnectChan)
		broker.reconnectChan = nil
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
	println("[AMPS] message consumer started successfully")

	go func(localHub *sentry.Hub) {
		localHub.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("goroutine", "messageConsumer")
		})

		messageCount := 0
		for d := range messages {
			if broker.isShuttingDown {
				break
			}

			messageCount++
			if messageCount == 1 {
				println("[AMPS] processing first message - system fully operational")
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

// Healthy checks the health of the broker with comprehensive validation
func (broker *AMQPBroker) Healthy() bool {
	broker.connMutex.Lock()
	defer broker.connMutex.Unlock()

	broker.lastHealthCheck = time.Now()

	// Check if we're shutting down
	if broker.isShuttingDown {
		return false
	}

	// During initial startup, be more lenient - don't report healthy until fully initialized
	if time.Since(broker.startupTime) < 10*time.Second && !broker.fullyInitialized {
		return false
	}

	// Check basic connection state
	if !broker.connected || broker.connection == nil {
		broker.consecutiveFailures++
		return false
	}

	// Check if connection is actually closed
	if broker.connection.IsClosed() {
		broker.consecutiveFailures++
		// Trigger reconnection if not already in progress
		select {
		case broker.reconnectChan <- true:
			println("[AMPS] health check detected closed connection, triggering reconnect")
		default:
		}
		return false
	}

	// Check channels are available and not closed
	if broker.consumeChannel == nil || broker.publishChannel == nil {
		broker.consecutiveFailures++
		return false
	}

	// Test if channels are still functional by checking if they're closed
	consumeChannelClosed := false
	publishChannelClosed := false

	select {
	case <-broker.consumeChannel.NotifyClose(make(chan *amqp.Error, 1)):
		consumeChannelClosed = true
	default:
	}

	select {
	case <-broker.publishChannel.NotifyClose(make(chan *amqp.Error, 1)):
		publishChannelClosed = true
	default:
	}

	if consumeChannelClosed || publishChannelClosed {
		broker.consecutiveFailures++
		println("[AMPS] health check detected closed channels, triggering reconnect")
		select {
		case broker.reconnectChan <- true:
		default:
		}
		return false
	}

	// If we're not fully initialized yet, wait
	if !broker.fullyInitialized {
		return false
	}

	// Give a small grace period after full initialization to avoid startup race conditions
	if time.Since(broker.lastConnected) < 500*time.Millisecond {
		return false
	}

	// If we've had too many consecutive failures recently, be more cautious
	if broker.consecutiveFailures > 3 && time.Since(broker.lastConnected) < 30*time.Second {
		return false
	}

	// Reset failure count on successful health check
	broker.consecutiveFailures = 0
	return true
}

// GetHealthDetails returns detailed health information for monitoring
func (broker *AMQPBroker) GetHealthDetails() map[string]interface{} {
	broker.connMutex.RLock()
	defer broker.connMutex.RUnlock()

	details := map[string]interface{}{
		"connected":              broker.connected,
		"running":                broker.running,
		"fully_initialized":      broker.fullyInitialized,
		"startup_time":           broker.startupTime.Format(time.RFC3339),
		"last_connected":         broker.lastConnected.Format(time.RFC3339),
		"last_reconnect_attempt": broker.lastReconnectAttempt.Format(time.RFC3339),
		"reconnect_count":        broker.reconnectCount,
		"consecutive_failures":   broker.consecutiveFailures,
		"last_health_check":      broker.lastHealthCheck.Format(time.RFC3339),
		"is_shutting_down":       broker.isShuttingDown,
	}

	if broker.connection != nil {
		details["connection_closed"] = broker.connection.IsClosed()
	} else {
		details["connection_closed"] = true
	}

	details["consume_channel_available"] = broker.consumeChannel != nil
	details["publish_channel_available"] = broker.publishChannel != nil

	if broker.lastConnected.IsZero() {
		details["uptime_seconds"] = 0
	} else {
		details["uptime_seconds"] = time.Since(broker.lastConnected).Seconds()
	}

	return details
}

// startHealthMonitor runs a periodic health check to detect missed connection drops
func (broker *AMQPBroker) startHealthMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if broker.isShuttingDown {
				return
			}

			broker.connMutex.RLock()
			connected := broker.connected
			connection := broker.connection
			lastConnected := broker.lastConnected
			broker.connMutex.RUnlock()

			// If we think we're connected but connection is actually closed
			if connected && connection != nil && connection.IsClosed() {
				println("[AMPS] health monitor detected stale connection, triggering reconnect")
				select {
				case broker.reconnectChan <- true:
					println("[AMPS] health monitor reconnection signal sent")
				default:
					println("[AMPS] health monitor reconnection already queued")
				}
			}

			// If connection has been up for a while, do a simple publish test
			if connected && connection != nil && !connection.IsClosed() &&
				time.Since(lastConnected) > 60*time.Second {
				// Try a simple channel operation to verify connection health
				broker.connMutex.RLock()
				publishChannel := broker.publishChannel
				broker.connMutex.RUnlock()

				if publishChannel != nil {
					// Test channel health by checking if it's closed
					select {
					case <-publishChannel.NotifyClose(make(chan *amqp.Error, 1)):
						println("[AMPS] health monitor detected closed channel, triggering reconnect")
						select {
						case broker.reconnectChan <- true:
							println("[AMPS] health monitor channel reconnection signal sent")
						default:
							println("[AMPS] health monitor channel reconnection already queued")
						}
					default:
						// Channel is healthy
					}
				}
			}

		case <-broker.shutdownChan:
			println("[AMPS] health monitor shutting down")
			return
		}
	}
}

// startReconnectionWatchdog monitors reconnection attempts and forces retry if stuck
func (broker *AMQPBroker) startReconnectionWatchdog() {
	ticker := time.NewTicker(60 * time.Second) // Check every minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if broker.isShuttingDown {
				return
			}

			broker.connMutex.RLock()
			connected := broker.connected
			lastReconnect := broker.lastReconnectAttempt
			broker.connMutex.RUnlock()

			// If we're not connected and haven't attempted reconnection recently
			if !connected && (lastReconnect.IsZero() || time.Since(lastReconnect) > 90*time.Second) {
				println("[AMPS] reconnection watchdog: forcing reconnection attempt")
				broker.connMutex.Lock()
				broker.lastReconnectAttempt = time.Now()
				broker.connMutex.Unlock()

				// Force a reconnection signal
				select {
				case broker.reconnectChan <- true:
					println("[AMPS] watchdog reconnection signal sent")
				default:
					// Channel might be full, drain and retry
					for len(broker.reconnectChan) > 0 {
						<-broker.reconnectChan
					}
					broker.reconnectChan <- true
					println("[AMPS] watchdog forced reconnection signal after draining")
				}
			}

		case <-broker.shutdownChan:
			println("[AMPS] reconnection watchdog shutting down")
			return
		}
	}
}
