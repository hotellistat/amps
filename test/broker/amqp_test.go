package test

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
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
	err := godotenv.Load()

	if err != nil {
		t.Fatal(err.Error())
	}

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

	if !broker.IsRunning() {
		t.Error("Broker should be running after Initialization")
	}
}
