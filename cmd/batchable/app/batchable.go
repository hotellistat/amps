package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Run is the primary entrypoint of the batchable application
func Run(conf *config.Config) {
	defer sentry.Recover()
	printBanner(*conf)

	brokerTypes := map[string]broker.Shim{
		"amqp": &broker.AMQPBroker{},
	}

	broker, ok := brokerTypes[conf.BrokerType]

	if !ok {
		println("Could not find broker type:", conf.BrokerType)
		os.Exit(1)
	}

	var manifestMutex = &sync.Mutex{}

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := job.NewManifest(conf.MaxConcurrency)

	// Initialize a new broker instance.
	broker.Initialize(*conf, &jobManifest)

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if conf.JobTimeout != 0 {
		tickInterval, _ := time.ParseDuration("1000ms")
		ticker := time.NewTicker(tickInterval)

		go Watchdog(ticker, conf, &jobManifest, &broker, manifestMutex)
	}
	// Info endpoint
	if conf.MetricsEnabled {
		metricsServer := http.NewServeMux()
		metricsServer.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(fmt.Sprint(":", conf.MetricsPort), metricsServer)
	}

	go func(localHub *sentry.Hub) {
		batchableServer := http.NewServeMux()

		// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
		batchableServer.HandleFunc("/acknowledge", func(w http.ResponseWriter, req *http.Request) {
			if req.Method != "POST" {
				fmt.Fprintf(w, "Only POST is allowed")
				return
			}
			err := JobAcknowledge(w, req, conf, &jobManifest, &broker)
			if err != nil {
				localHub.CaptureException(err)
			}
		})

		// This endpoint handles job deletion
		batchableServer.HandleFunc("/reject", func(w http.ResponseWriter, req *http.Request) {
			if req.Method != "POST" {
				fmt.Fprintf(w, "Only POST is allowed")
				return
			}
			err := JobReject(w, req, conf, &jobManifest, &broker)
			if err != nil {
				localHub.CaptureException(err)
			}
		})

		// This endpoint handles job deletion
		batchableServer.HandleFunc("/publish", func(w http.ResponseWriter, req *http.Request) {
			if req.Method != "POST" {
				fmt.Fprintf(w, "Only POST is allowed")
				return
			}
			err := JobPublish(w, req, conf, &jobManifest, &broker)
			if err != nil {
				localHub.CaptureException(err)
			}
		})

		// Health check so the container can be killed if unhealthy
		batchableServer.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
			brokerHealthy := broker.Healthy()
			if brokerHealthy {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("UNHEALTHY"))
			}
		})

		http.ListenAndServe(fmt.Sprint(":", conf.Port), batchableServer)

	}(sentry.CurrentHub().Clone())

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
