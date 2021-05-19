package test

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

// func TestAmqpConnect(t *testing.T) {
// 	err := godotenv.Load()
// 	if err != nil {
// 		t.Fatal(err.Error())
// 	}

// 	broker := broker.AMQPBroker{}

// 	timeout := time.After(3 * time.Second)
// 	done := make(chan bool)
// 	errorChan := make(chan error)
// 	go func() {
// 		broker.amqpConnect(os.Getenv("TEST_BROKER_DSN"), errorChan)
// 		done <- true
// 	}()

// 	select {
// 	case err := <-errorChan:
// 		t.Fatal(err.Error())
// 	case <-timeout:
// 		t.Fatal("Test didn't finish in time")
// 	case <-done:
// 	}
// }

func TestAmqpInitialize(t *testing.T) {

	godotenv.Load()

	broker := broker.AMQPBroker{}

	config := config.Config{
		BrokerDsn:               os.Getenv("TEST_AMQP_BROKER_DSN"),
		WorkerID:                "test-worker",
		BrokerType:              "amqp",
		BrokerSubject:           "test-subject",
		Debug:                   true,
		JobTimeout:              time.Duration(1 * time.Minute),
		MaxConcurrency:          2,
		WorkloadAddress:         "localhost:8080",
		WorkloadResponseTimeout: time.Duration(10 * time.Second),
	}

	jobManifest := job.NewManifest(2)

	broker.Initialize(config, &jobManifest)

	conn, err := amqp.Dial(os.Getenv("TEST_AMQP_BROKER_DSN"))

	if err != nil {
		t.Fatal(err.Error())
	}

	testChannel, testErr := conn.Channel()

	if testErr != nil {
		t.Fatal(testErr.Error())
	}

	_, queueDeclaredErr := testChannel.QueueInspect("test-subject")

	if queueDeclaredErr != nil {
		t.Fatal("Broker did not declare queue")
	}

	queue, queueErr := testChannel.QueueDeclare(
		"test-subject",
		true,
		false,
		false,
		false,
		nil,
	)

	if queueErr != nil {
		t.Fatal(queueErr.Error())
	}

	t.Log(queue.Consumers)

	if queue.Consumers < 1 {
		t.Error("Broker did not initialize consumer")
	}

	if !broker.IsRunning() {
		t.Error("Broker should be running after Initialization")
	}
}
