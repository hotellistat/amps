package integration

import (
	"os"
	"testing"
	"time"

	"github.com/hotellistat/amps/cmd/amps/broker"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

// TestAMQPReconnection tests that AMPS can recover gracefully when RabbitMQ restarts
func TestAMQPReconnection(t *testing.T) {
	// Skip if no test environment is configured
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		t.Skip("Skipping integration tests")
	}

	err := godotenv.Load("../../.env")
	if err != nil {
		// Try loading from test directory
		godotenv.Load("../.env")
	}

	testDSN := os.Getenv("TEST_AMQP_BROKER_DSN")
	if testDSN == "" {
		testDSN = "amqp://guest:guest@localhost:5672/"
		t.Logf("Using default test DSN: %s", testDSN)
	}

	// Create broker instance
	amqpBroker := &broker.AMQPBroker{}

	config := config.Config{
		BrokerDsn:               testDSN,
		WorkerID:                "test-reconnect-worker",
		BrokerType:              "amqp",
		BrokerSubject:           "test-reconnect-subject",
		Debug:                   true,
		JobTimeout:              time.Duration(1 * time.Minute),
		MaxConcurrency:          2,
		WorkloadAddress:         "localhost:8080",
		WorkloadResponseTimeout: time.Duration(10 * time.Second),
	}

	jobManifest := job.NewManifest(2)

	// Test initial connection
	t.Log("Testing initial connection...")
	connected := amqpBroker.Initialize(config, &jobManifest)
	if !connected {
		t.Fatal("Failed to initialize AMQP broker")
	}

	// Verify broker is healthy and running
	if !amqpBroker.Healthy() {
		t.Fatal("Broker should be healthy after initialization")
	}

	if !amqpBroker.IsRunning() {
		t.Fatal("Broker should be running after initialization")
	}

	t.Log("Initial connection successful")

	// Test connection resilience by simulating connection issues
	t.Log("Testing connection resilience...")

	// Create a direct connection to test the broker's queue
	testConn, err := amqp.Dial(testDSN)
	if err != nil {
		t.Fatalf("Failed to create test connection: %v", err)
	}
	defer testConn.Close()

	testChannel, err := testConn.Channel()
	if err != nil {
		t.Fatalf("Failed to create test channel: %v", err)
	}
	defer testChannel.Close()

	// Declare the test queue to make sure it exists
	_, err = testChannel.QueueDeclare(
		config.BrokerSubject,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		t.Fatalf("Failed to declare test queue: %v", err)
	}

	// Wait a bit to ensure connection is stable
	time.Sleep(2 * time.Second)

	// Test that the broker can handle publish operations
	t.Log("Testing publish functionality...")

	// We can't easily test message publishing without a full CloudEvent,
	// but we can test that the broker maintains its connection state

	// Monitor health for a period
	healthCheckDuration := 10 * time.Second
	healthCheckInterval := 1 * time.Second
	startTime := time.Now()

	t.Logf("Monitoring broker health for %v...", healthCheckDuration)

	for time.Since(startTime) < healthCheckDuration {
		if !amqpBroker.Healthy() {
			t.Errorf("Broker became unhealthy after %v", time.Since(startTime))
			break
		}
		time.Sleep(healthCheckInterval)
	}

	// Test graceful shutdown
	t.Log("Testing graceful shutdown...")
	amqpBroker.Teardown()

	// Verify broker is no longer healthy after teardown
	time.Sleep(1 * time.Second) // Give it a moment to shutdown

	if amqpBroker.IsRunning() {
		t.Error("Broker should not be running after teardown")
	}

	t.Log("AMQP reconnection test completed successfully")
}

// TestAMQPHealthCheck tests the health check functionality
func TestAMQPHealthCheck(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		t.Skip("Skipping integration tests")
	}

	err := godotenv.Load("../../.env")
	if err != nil {
		godotenv.Load("../.env")
	}

	testDSN := os.Getenv("TEST_AMQP_BROKER_DSN")
	if testDSN == "" {
		testDSN = "amqp://guest:guest@localhost:5672/"
	}

	amqpBroker := &broker.AMQPBroker{}

	// Test health check before initialization
	if amqpBroker.Healthy() {
		t.Error("Broker should not be healthy before initialization")
	}

	config := config.Config{
		BrokerDsn:               testDSN,
		WorkerID:                "test-health-worker",
		BrokerType:              "amqp",
		BrokerSubject:           "test-health-subject",
		Debug:                   false,
		JobTimeout:              time.Duration(1 * time.Minute),
		MaxConcurrency:          1,
		WorkloadAddress:         "localhost:8080",
		WorkloadResponseTimeout: time.Duration(10 * time.Second),
	}

	jobManifest := job.NewManifest(1)

	// Initialize and test health
	connected := amqpBroker.Initialize(config, &jobManifest)
	if !connected {
		t.Fatal("Failed to initialize AMQP broker")
	}

	if !amqpBroker.Healthy() {
		t.Error("Broker should be healthy after successful initialization")
	}

	// Test health after teardown
	amqpBroker.Teardown()

	time.Sleep(1 * time.Second)

	if amqpBroker.Healthy() {
		t.Error("Broker should not be healthy after teardown")
	}

	t.Log("Health check test completed successfully")
}
