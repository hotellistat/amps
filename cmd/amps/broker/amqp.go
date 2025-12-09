package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/getsentry/sentry-go"
	"github.com/hotellistat/amps/cmd/amps/cloudevent"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
	"github.com/hotellistat/amps/cmd/amps/workload"
	"github.com/streadway/amqp"
)

//
// ────────────────────────────────────────────────────────────────────────────────
//   AMQP Broker Struct
// ────────────────────────────────────────────────────────────────────────────────
//
// The AMQPBroker struct is the core of AMPS’s RabbitMQ integration. Each background
// service runs an instance of this broker beside it. The broker handles:
//
// - Connecting and reconnecting to RabbitMQ
// - Publishing and consuming messages
// - Managing goroutines safely via context cancellation
// - Monitoring connection health and triggering reconnections when needed
//
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
	lastConnected        time.Time
	reconnectCount       int
	lastHealthCheck      time.Time
	consecutiveFailures  int
	fullyInitialized     bool
	startupTime          time.Time
	lastReconnectAttempt time.Time

	ctx    context.Context
	cancel context.CancelFunc
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Connection Establishment with Exponential Backoff
// ────────────────────────────────────────────────────────────────────────────────
//
// Continuously tries to connect to the AMQP broker using exponential backoff.
// Exits gracefully if the context is canceled (during shutdown).
//

func (broker *AMQPBroker) amqpConnect(uri string, errorChan chan error, localHub *sentry.Hub) *amqp.Connection {
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnect")
	})

	attempt := 0
	maxBackoff := 30 * time.Second
	baseBackoff := 1 * time.Second

	for {
		select {
		case <-broker.ctx.Done():
			println("[AMPS] connection attempts cancelled via context")
			return nil
		default:
		}

		conn, err := amqp.Dial(uri)
		if err == nil {
			// --- DIAGNOSTIC ---
			println("[AMPS][DIAG] amqp_dial_success goroutines=", runtime.NumGoroutine())
			return conn
		}
		
		attempt++
		backoff := time.Duration(math.Min(float64(baseBackoff)*math.Pow(2, float64(attempt)), float64(maxBackoff)))

		localHub.CaptureException(err)
		println("[AMPS] dial exception:", err.Error(), "- retrying in", backoff, "(attempt", attempt, ")")

		// Prevent blocking if the error channel isn’t being read
		select {
		case errorChan <- err:
		default:
		}

		select {
		case <-time.After(backoff):
		case <-broker.ctx.Done():
			println("[AMPS] connection attempts cancelled due to shutdown")
			return nil
		}
	}
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Channel Creation with Retry Logic
// ────────────────────────────────────────────────────────────────────────────────
//
// Creates consumer and publisher channels from the current AMQP connection.
// Uses retry logic to handle transient errors, sets QoS (prefetch) for concurrency.
//

func (broker *AMQPBroker) createChannels(localHub *sentry.Hub) error {
	maxRetries := 5
	baseDelay := 1 * time.Second

	for retry := 0; retry < maxRetries; retry++ {
		select {
		case <-broker.ctx.Done():
			return errors.New("shutting down")
		default:
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
		// --- DIAGNOSTIC ---
		println("[AMPS][DIAG] channels_opened goroutines=", runtime.NumGoroutine())
		
		broker.connMutex.Unlock()

		println("[AMPS] successfully created AMQP channels - starting consumer...")
		return nil
	}

	return errors.New("failed to create channels after all retries")
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Connection Routine (Main Loop)
// ────────────────────────────────────────────────────────────────────────────────
//
// Handles the entire AMQP connection lifecycle. Responds to reconnection signals,
// cleans up stale resources, and safely reinitializes channels and consumers.
//

func (broker *AMQPBroker) amqpConnectRoutine(uri string, connected chan bool) {
	localHub := sentry.CurrentHub().Clone()
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "amqpConnectRoutine")
	})

	reconnectionAttempt := 0

	for {
		select {
		case <-broker.reconnectChan:
			reconnectionAttempt++
			broker.reconnectCount++
			broker.connMutex.Lock()
			broker.lastReconnectAttempt = time.Now()
			broker.connMutex.Unlock()
			println("[AMPS] reconnection signal received (attempt", reconnectionAttempt, ") - Goroutines:", runtime.NumGoroutine())
			// --- DIAGNOSTIC ---
			broker.jobManifest.Mutex.RLock()
			jobCount := len(broker.jobManifest.Jobs)
			broker.jobManifest.Mutex.RUnlock()
			println("[AMPS][DIAG] reconnect_attempt=", reconnectionAttempt,
				" goroutines=", runtime.NumGoroutine(),
				" jobs=", jobCount)
					case <-broker.ctx.Done():
						println("[AMPS] reconnection routine shutting down")
						return
					}

		// Reset all resources before reconnecting
		broker.connMutex.Lock()
		broker.running = false
		broker.connected = false
		broker.fullyInitialized = false
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
		
		broker.connMutex.Unlock()

		// Remove stale jobs from manifest (will be redelivered later)
		broker.jobManifest.Mutex.Lock()
		staleCount := 0
		for id, job := range broker.jobManifest.Jobs {
			if job.Delivery != nil {
				delete(broker.jobManifest.Jobs, id)
				staleCount++
			}
		}
		broker.jobManifest.Mutex.Unlock()
		if staleCount > 0 {
			println("[AMPS] cleaned up", staleCount, "stale jobs from manifest - they will be redelivered")
		}

		connectErrorChan := make(chan error, 10)
		go func() {
			for {
				select {
				case <-broker.ctx.Done():
					return
				case err, ok := <-connectErrorChan:
					if !ok {
						return
					}
					if err != nil {
						fmt.Println("[AMPS] connection error:", err.Error())
					}
				}
			}
		}()

		conn := broker.amqpConnect(uri, connectErrorChan, localHub)
		close(connectErrorChan) // FIX: Ensures goroutine exits cleanly

		if conn == nil {
			println("[AMPS] connection attempt failed - shutdown was requested")
			return
		}

		broker.connMutex.Lock()
		broker.connection = conn
		broker.connMutex.Unlock()

		closeNotify := make(chan *amqp.Error, 1)
		conn.NotifyClose(closeNotify)

		// Watches for unexpected connection closure
		go func(connectionAttempt int) {
			select {
			case closeErr := <-closeNotify:
				if closeErr != nil && broker.ctx.Err() == nil {
					broker.connMutex.Lock()
					uptime := time.Since(broker.lastConnected)
					broker.connMutex.Unlock()
					println("[AMPS] connection", connectionAttempt, "closed after", uptime, ":", closeErr.Error())
					// --- DIAGNOSTIC ---
					println("[AMPS][DIAG] NotifyClose: connection closed unexpectedly:",
						closeErr.Error(), 
						" goroutines=", runtime.NumGoroutine())
					select {
					case broker.reconnectChan <- true:
					default:
					}
				}
			case <-broker.ctx.Done():
				return
			}
		}(reconnectionAttempt)

		// Create channels
		err := broker.createChannels(localHub)
		if err != nil {
			localHub.CaptureException(err)
			println("[AMPS] failed to create channels:", err.Error(), "- will retry")
			select {
			case <-time.After(2 * time.Second):
			case <-broker.ctx.Done():
				return
			}
			select {
			case broker.reconnectChan <- true:
			default:
			}
			continue
		}

		// Mark broker as connected
		broker.connMutex.Lock()
		broker.connected = true
		broker.lastConnected = time.Now()
		broker.connMutex.Unlock()

		// Start consuming
		startErr := broker.Start()
		if startErr != nil {
			localHub.CaptureException(startErr)
			println("[AMPS][DIAG] consumer_start_failed err=", startErr.Error(), " goroutines=", runtime.NumGoroutine())
			select {
			case <-time.After(2 * time.Second):
			case <-broker.ctx.Done():
				return
			}
			select {
			case broker.reconnectChan <- true:
			default:
			}
			continue
		}

		broker.connMutex.Lock()
		broker.fullyInitialized = true
		broker.connMutex.Unlock()
		println("[AMPS] broker fully initialized and ready to process messages")

		select {
		case connected <- true:
		default:
		}
	}
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Start Consumer
// ────────────────────────────────────────────────────────────────────────────────
//
// Creates a consumer on the broker’s queue, listens for messages, and passes each
// message to the handler function. This runs as a goroutine and respects context
// cancellation for graceful shutdown.
//

func (broker *AMQPBroker) Start() error {	
	broker.busy.Lock()
	defer broker.busy.Unlock()
	diagnosticsEnabled = broker.config.PprofEnabled
	
	// Hold WRITE lock across the entire Start critical section
    broker.connMutex.Lock()
    defer broker.connMutex.Unlock()

	// --- 1. Check state atomically ---
	if broker.running  {
		return nil // Already running
	}

	consumeChannel := broker.consumeChannel
    connected := broker.connected

	// --- 2. Validate connection state ---
	if !connected || consumeChannel == nil {
		return errors.New("not connected or channel not available")
	}

	// --- 3. Create consumer under the SAME lock ---
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
		fmt.Println("[AMPS] Could not start consumer:", err.Error())
		println("[AMPS][DIAG] consume_start_failed goroutines=", runtime.NumGoroutine())
		return err
	}
	// --- 4. Update state atomically ---	
	broker.running = true	
	println("[AMPS] message consumer started successfully")
	// --- DIAGNOSTIC ---
	println("[AMPS][DIAG] consumer goroutine START goroutines=", runtime.NumGoroutine())
	 // --- 5. Start background goroutine (outside the lock) ---
	go func() {
	 // log exit no matter how the goroutine ends
		defer func() {
			println("[AMPS][DIAG] consumer goroutine EXIT goroutines=", runtime.NumGoroutine())
		}()

		for {
			select {
			case <-broker.ctx.Done():
				println("[AMPS] message consumer stopped by context")
				 // --- DIAGNOSTIC ---
			    println("[AMPS][DIAG] consumer_exit_reason=ctx_done goroutines=", runtime.NumGoroutine())
				return
			case d, ok := <-messages:
				if !ok {
					println("[AMPS] message channel closed")
					// --- DIAGNOSTIC ---
      				println("[AMPS][DIAG] consumer_exit_reason=message_channel_closed goroutines=", runtime.NumGoroutine())      
					return
				}
				err := broker.messageHandler(d)
				if err != nil {
					println("[AMPS] message handler error:", err.Error())
				}
			}
		}
	}()
	return nil
}
//
// ────────────────────────────────────────────────────────────────────────────────
//   Initialize Broker
// ────────────────────────────────────────────────────────────────────────────────
//
// This sets up the broker, launches the main connection loop, health monitor,
// and reconnection watchdog. It also sends the first reconnect signal to trigger
// initial connection to RabbitMQ.
//

func (broker *AMQPBroker) Initialize(config config.Config, jobManifest *job.Manifest) bool {
	broker.config = config
	broker.jobManifest = jobManifest
	broker.connMutex = &sync.RWMutex{}
	broker.fullyInitialized = false
	broker.busy = &sync.Mutex{}
	broker.startupTime = time.Now()

	// Create a cancellable context to control all goroutines
	broker.ctx, broker.cancel = context.WithCancel(context.Background())

	// Buffered channel prevents blocking on rapid reconnect signals
	broker.reconnectChan = make(chan bool, 20)
	connectedChan := make(chan bool, 1)

	// Launch the main connection management routine
	go broker.amqpConnectRoutine(config.BrokerDsn, connectedChan)

	// Trigger the initial connection
	broker.reconnectChan <- true

	// Start background monitors
	go broker.startHealthMonitor()
	go broker.startReconnectionWatchdog()

	// Wait until connected or timeout
	select {
	case success := <-connectedChan:
		return success
	case <-time.After(30 * time.Second):
		println("[AMPS] timeout waiting for initial connection")
		return false
	}
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Publish with Retry
// ────────────────────────────────────────────────────────────────────────────────
//
// Publishes a message to the queue with exponential backoff retries.
// Each attempt checks for connection status and context cancellation.
//

func (broker *AMQPBroker) publishWithRetry(routingKey string, body []byte, maxRetries int) error {
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-broker.ctx.Done():
			return errors.New("shutting down")
		default:
		}

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
			return nil
		}

		println("[AMPS] publish failed, attempt", attempt+1, ":", err.Error())
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}

	return errors.New("failed to publish after all retries")
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Teardown Broker
// ────────────────────────────────────────────────────────────────────────────────
//
// Gracefully shuts down the broker and all goroutines by canceling the context,
// closing channels and connections, and cleaning up state safely.
//

func (broker *AMQPBroker) Teardown() {
	println("[AMPS] tearing down broker")

	// Cancel context to stop all background goroutines
	if broker.cancel != nil {
		broker.cancel()
	}

	// Give goroutines a brief moment to exit cleanly
	time.Sleep(100 * time.Millisecond)
	// --- DIAGNOSTIC ---
	println("[AMPS][DIAG] broker_teardown goroutines=", runtime.NumGoroutine())
	
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

//
// ────────────────────────────────────────────────────────────────────────────────
//   Job Delivery Wrapper
// ────────────────────────────────────────────────────────────────────────────────
//
// Provides simple wrappers for ACK/NACK operations on consumed messages.
//

type AmqpMessageWrapper struct {
	message amqp.Delivery
}

func (wrapper AmqpMessageWrapper) Ack(multiple bool) error {
	return wrapper.message.Ack(multiple)
}

func (wrapper AmqpMessageWrapper) Nack(multiple, requeue bool) error {
	return wrapper.message.Nack(multiple, requeue)
}

var diagnosticsEnabled = false
//
// ────────────────────────────────────────────────────────────────────────────────
//   Message Handler
// ────────────────────────────────────────────────────────────────────────────────
//
// Processes each incoming message:
// - Unmarshals it into a CloudEvent
// - Checks if the job already exists (deduplication)
// - Inserts it into the job manifest
// - Calls workload.Trigger() to process
// - On error, NACKs and reschedules the job
//

func (broker *AMQPBroker) messageHandler(msg amqp.Delivery) error {
	if diagnosticsEnabled {
		// --- DIAGNOSTIC: measure handler duration (minimal impact) ---
		start := time.Now()
		defer func() {
			dur := time.Since(start)
			if dur > 2*time.Second {
				println("[AMPS][DIAG] slow_message_handler duration=", dur.String(),
					" goroutines=", runtime.NumGoroutine())
			}
		}()
	}
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
		msg.Nack(false, false)
		return errors.New("[AMPS] Job ID: " + eventID + " already exists")
	}

	messageWrapper := AmqpMessageWrapper{msg}
	broker.jobManifest.InsertJobWithDelivery(eventID, messageWrapper)
	broker.jobManifest.Mutex.Unlock()

	// Send job to workload endpoint
	workloadErr := workload.Trigger(event, broker.config)
	if workloadErr != nil {
		fmt.Println("[AMPS]", workloadErr.Error())
		println("[AMPS] Rejecting job for rescheduling")

		broker.jobManifest.Mutex.Lock()
		if !broker.jobManifest.HasJob(eventID) {
			broker.jobManifest.Mutex.Unlock()
			println("[AMPS] Job ID:", eventID, "does not exist in the manifest")
			return nil
		}

		broker.jobManifest.DeleteJob(eventID)
		broker.jobManifest.Mutex.Unlock()

		// Negative acknowledgment and requeue
		nackErr := msg.Nack(false, true)
		if nackErr != nil {
			println("[AMPS] error nacking message:", nackErr.Error())
		}
	}
	return nil
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Stop Consumer
// ────────────────────────────────────────────────────────────────────────────────
//
// Cancels message consumption gracefully without shutting down the broker itself.
//

func (broker *AMQPBroker) Stop() error {
	if broker.busy == nil {
		return nil
	}

	broker.busy.Lock()
	defer broker.busy.Unlock()

	broker.connMutex.RLock()
	running := broker.running
	broker.connMutex.RUnlock()
	if !running {

		return nil
	}

	broker.connMutex.RLock()
	consumeChannel := broker.consumeChannel
	broker.connMutex.RUnlock()

	if consumeChannel != nil {
		err := consumeChannel.Cancel(broker.config.WorkerID, false)
		if err != nil {
			fmt.Println("[AMPS] Could not cancel consumer:", err.Error())
			return err
		}
	}

	broker.connMutex.Lock()
	broker.running = false
	broker.connMutex.Unlock()
	return nil
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Publish CloudEvent Message
// ────────────────────────────────────────────────────────────────────────────────
//
// Marshals a CloudEvent into JSON and publishes it to the AMQP queue.
//

func (broker *AMQPBroker) PublishMessage(event event.Event) error {
	encodedData, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		return errors.New("could not marshal cloudevent while publishing")
	}

	err := broker.publishWithRetry(event.Context.GetType(), encodedData, 3)
	if err != nil {
		fmt.Println("[AMPS] Could not publish result:", err.Error())
		return err
	}

	return nil
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Health Check
// ────────────────────────────────────────────────────────────────────────────────
//
// Performs an in-depth health check of the broker’s state and triggers a
// reconnection signal if any part of the connection is unhealthy.
//

func (broker *AMQPBroker) Healthy() bool {
	broker.connMutex.Lock()
	defer broker.connMutex.Unlock()

	broker.lastHealthCheck = time.Now()

	// Context canceled = shutting down
	if broker.ctx.Err() != nil {
		return false
	}

	// Allow startup grace period
	if time.Since(broker.startupTime) < 10*time.Second && !broker.fullyInitialized {
		return false
	}

	if !broker.connected || broker.connection == nil {
		broker.consecutiveFailures++
		return false
	}

	if broker.connection.IsClosed() {
		broker.consecutiveFailures++
		select {
		case broker.reconnectChan <- true:
			println("[AMPS] health check detected closed connection, triggering reconnect")
		default:
		}
		return false
	}

	if broker.consumeChannel == nil || broker.publishChannel == nil {
		broker.consecutiveFailures++
		return false
	}

	if !broker.fullyInitialized {
		return false
	}

	broker.consecutiveFailures = 0
	return true
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Health Details Snapshot
// ────────────────────────────────────────────────────────────────────────────────
//
// Returns a structured map of the broker’s current state for metrics or API
// inspection.
//

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
		"is_shutting_down":       broker.ctx.Err() != nil,
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

//
// ────────────────────────────────────────────────────────────────────────────────
//   Health Monitor (Background Goroutine)
// ────────────────────────────────────────────────────────────────────────────────
//
// Periodically checks connection health and issues reconnection signals
// if the connection is found to be closed unexpectedly.
//

func (broker *AMQPBroker) startHealthMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			broker.connMutex.RLock()
			connected := broker.connected
			connection := broker.connection
			broker.connMutex.RUnlock()

			if connected && (connection == nil || connection.IsClosed()) {
				println("[AMPS] health monitor detected closed connection, triggering reconnect")
				select {
				case broker.reconnectChan <- true:
					println("[AMPS] health monitor reconnection signal sent")
				default:
					println("[AMPS] health monitor reconnection already queued")
				}
			}
		case <-broker.ctx.Done():
			println("[AMPS] health monitor shutting down")
			return
		}
	}
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   Reconnection Watchdog (Failsafe Timer)
// ────────────────────────────────────────────────────────────────────────────────
//
// Acts as a failsafe that triggers a forced reconnection if the broker remains
// disconnected or stuck for too long (e.g., network partitions).
//

func (broker *AMQPBroker) startReconnectionWatchdog() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			broker.connMutex.RLock()
			connected := broker.connected
			lastReconnect := broker.lastReconnectAttempt
			broker.connMutex.RUnlock()

			if !connected && (lastReconnect.IsZero() || time.Since(lastReconnect) > 90*time.Second) {
				println("[AMPS] reconnection watchdog: forcing reconnection attempt")
				broker.connMutex.Lock()
				broker.lastReconnectAttempt = time.Now()
				broker.connMutex.Unlock()

				select {
				case broker.reconnectChan <- true:
					println("[AMPS] watchdog reconnection signal sent")
				default:
					println("[AMPS] watchdog reconnection signal already queued")
				}
			}
		case <-broker.ctx.Done():
			println("[AMPS] reconnection watchdog shutting down")
			return
		}
	}
}


// IsRunning returns true if the broker's consumer loop is currently active.
// A read lock is used for thread-safe access to the shared 'running' flag.
func (broker *AMQPBroker) IsRunning() bool {
	if broker == nil || broker.connMutex == nil {
		return false
	}
	broker.connMutex.RLock()
	defer broker.connMutex.RUnlock()
	return broker.running
}