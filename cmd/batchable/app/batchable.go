package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
)

// Run is the primary entrypoint of the batchable application
func Run() {
	conf := config.New()

	printBanner(*conf)

	brokerTypes := map[string]broker.Shim{
		"amqp": &broker.AMQPBroker{},
	}

	broker, ok := brokerTypes[conf.BrokerType]

	if !ok {
		log.Fatal("Could not find broker type:", conf.BrokerType)
	}

	var manifestMutex = &sync.Mutex{}

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := job.NewManifest(conf.MaxConcurrency)

	// Initialize a new broker instance.
	broker.Initialize(*conf, &jobManifest)

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if conf.JobTimeout != 0 {
		go Watchdog(conf, &jobManifest, &broker, manifestMutex)
	}

	// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
	http.HandleFunc("/complete", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			fmt.Fprintf(w, "Only POST is allowed")
			return
		}
		JobComplete(w, req, conf, &jobManifest, &broker)
	})

	// This endpoint handles job deletion
	http.HandleFunc("/delete", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			fmt.Fprintf(w, "Only POST is allowed")
			return
		}
		JobDelete(w, req, conf, &jobManifest, &broker)
	})

	// Health check so the container can be killed if unhealthy
	http.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		brokerHealthy := broker.Healthy()
		if brokerHealthy {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("UNHEALTHY"))
		}
	})

	go http.ListenAndServe(":4000", nil)

	// General signal handling to teardown the worker
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for signal := range signalChan {
			println("[batchable] signal:", signal.String())
			broker.Evacuate()
			broker.Teardown()
			cleanupDone <- true
		}
	}()
	<-cleanupDone

}
