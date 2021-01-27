package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/joho/godotenv"
	"github.com/nats-io/stan.go"
)

// A Job represents one current workitem that needs to be processed

// BrokerShim is an abstracion of the functions that each broker shim needs to implement
type BrokerShim interface {
	Initialize(config.Config)
	Teardown()
	Start(stan.MsgHandler)
	Stop()
	PublishResult(config.Config, event.Event) error
}

// Run is the primary entrypoint of the batchable application
func Run() {
	godotenv.Load()

	conf := config.New()

	brokerTypes := map[string]BrokerShim{
		"nats": &broker.NatsBroker{},
	}

	broker, ok := brokerTypes[conf.BrokerType]

	if !ok {
		log.Fatal("Could not find broker type:", conf.BrokerType)
	}

	var manifestMutex = &sync.Mutex{}

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := NewJobManifest()

	// Initialize a new broker instance.
	broker.Initialize(*conf)

	// Create a new subscription for nats streaming
	broker.Start(func(msg *stan.Msg) {
		MessageHandler(msg, conf, &jobManifest, &broker)
	})

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if conf.JobTimeout != 0 {
		go Watchdog(conf, &jobManifest, &broker, manifestMutex)
	}

	// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
	http.HandleFunc("/checkout", func(w http.ResponseWriter, req *http.Request) {
		JobCheckout(w, req, conf, &jobManifest, &broker)
	})

	go http.ListenAndServe(":4000", nil)

	// General signal handling to teardown the worker
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for range signalChan {
			fmt.Printf("\nReceived an interrupt, closing connection...\n\n")
			broker.Teardown()

			cleanupDone <- true
		}
	}()
	<-cleanupDone

}
