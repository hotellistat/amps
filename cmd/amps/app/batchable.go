package app

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/hotellistat/AMPS/cmd/amps/broker"
	"github.com/hotellistat/AMPS/cmd/amps/config"
	"github.com/hotellistat/AMPS/cmd/amps/job"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Run is the primary entrypoint of the AMPS application
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
	if conf.JobTimeout > 0 {
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

	go func() {
		server := http.NewServeMux()
		sentryHandler := sentryhttp.New(sentryhttp.Options{})

		// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
		server.HandleFunc("/acknowledge", sentryHandler.HandleFunc(func(w http.ResponseWriter, req *http.Request) {
			localHub := sentry.GetHubFromContext(req.Context())

			if req.Method != "POST" {
				fmt.Fprintf(w, "Only POST is allowed")
				return
			}
			err := JobAcknowledge(w, req, conf, &jobManifest, &broker)
			if err != nil {
				localHub.CaptureException(err)
			}
		}))

		// This endpoint handles job deletion
		server.HandleFunc("/reject", sentryHandler.HandleFunc(func(w http.ResponseWriter, req *http.Request) {
			localHub := sentry.GetHubFromContext(req.Context())
			if req.Method != "POST" {
				fmt.Fprintf(w, "Only POST is allowed")
				return
			}
			err := JobReject(w, req, conf, &jobManifest, &broker)
			if err != nil {
				localHub.CaptureException(err)
			}
		}))

		// This endpoint handles job deletion
		server.HandleFunc("/publish", sentryHandler.HandleFunc(func(w http.ResponseWriter, req *http.Request) {
			localHub := sentry.GetHubFromContext(req.Context())
			if req.Method != "POST" {
				fmt.Fprintf(w, "Only POST is allowed")
				return
			}
			err := JobPublish(w, req, conf, &jobManifest, &broker)
			if err != nil {
				localHub.CaptureException(err)
			}
		}))

		// Health check so the container can be killed if unhealthy
		server.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
			brokerHealthy := broker.Healthy()
			if brokerHealthy {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("UNHEALTHY"))
			}
		})

		http.ListenAndServe(fmt.Sprint(":", conf.Port), server)

	}()

	// General signal handling to teardown the worker
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for signal := range signalChan {
			println("[AMPS] signal:", signal.String())
			broker.Evacuate()
			broker.Teardown()
			cleanupDone <- true
		}
	}()
	<-cleanupDone

}
