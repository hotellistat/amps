package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/hotellistat/amps/cmd/amps/broker"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HealthStatus represents the health status of AMPS
type HealthStatus struct {
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"timestamp"`
	Uptime     string    `json:"uptime"`
	BrokerType string    `json:"broker_type"`
	Connected  bool      `json:"connected"`
	JobCount   int       `json:"job_count"`
	Details    string    `json:"details,omitempty"`
}

var (
	startupTime = time.Now()
	isReady     = false
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
	brokerInitialized := broker.Initialize(*conf, &jobManifest)

	// Mark as ready only if broker initialized successfully
	isReady = brokerInitialized

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
		println("[AMPS] Metrics server started")
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

		// Kubernetes liveness probe - checks if the application is alive
		server.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
			handleHealthCheck(w, req, &broker, &jobManifest, conf, false)
		})

		// Kubernetes readiness probe - checks if the application is ready to serve traffic
		server.HandleFunc("/readyz", func(w http.ResponseWriter, req *http.Request) {
			handleHealthCheck(w, req, &broker, &jobManifest, conf, true)
		})

		// Legacy health endpoint for backward compatibility
		server.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
			handleHealthCheck(w, req, &broker, &jobManifest, conf, false)
		})

		// Detailed health endpoint with broker diagnostics
		server.HandleFunc("/health/detailed", func(w http.ResponseWriter, req *http.Request) {
			handleDetailedHealthCheck(w, req, &broker, &jobManifest, conf)
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
			broker.Teardown()
			cleanupDone <- true
		}
	}()
	<-cleanupDone

}

// handleHealthCheck provides comprehensive health checking for Kubernetes probes
func handleHealthCheck(w http.ResponseWriter, req *http.Request, broker *broker.Shim, jobManifest *job.Manifest, conf *config.Config, checkReadiness bool) {
	w.Header().Set("Content-Type", "application/json")

	brokerHealthy := (*broker).Healthy()
	uptime := time.Since(startupTime)

	jobManifest.Mutex.RLock()
	jobCount := jobManifest.Size()
	jobManifest.Mutex.RUnlock()

	status := HealthStatus{
		Timestamp:  time.Now(),
		Uptime:     uptime.String(),
		BrokerType: conf.BrokerType,
		Connected:  brokerHealthy,
		JobCount:   jobCount,
	}

	// Determine health based on type of check
	if checkReadiness {
		// Readiness check - must be ready AND healthy
		if isReady && brokerHealthy {
			status.Status = "ready"
			w.WriteHeader(http.StatusOK)
		} else {
			status.Status = "not ready"
			if !isReady {
				status.Details = "application still initializing"
			} else if !brokerHealthy {
				status.Details = "broker connection unhealthy"
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	} else {
		// Liveness check - application is alive if it can respond
		if brokerHealthy {
			status.Status = "healthy"
			w.WriteHeader(http.StatusOK)
		} else {
			status.Status = "unhealthy"
			status.Details = "broker connection failed - reconnection in progress"
			// Return 503 for liveness check failures so Kubernetes restarts the pod
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}

	// Log health check failures for debugging
	if status.Status != "healthy" && status.Status != "ready" {
		println(fmt.Sprintf("[AMPS] Health check failed: %s - %s", status.Status, status.Details))
	}

	json.NewEncoder(w).Encode(status)
}

// handleDetailedHealthCheck provides comprehensive health and diagnostic information
func handleDetailedHealthCheck(w http.ResponseWriter, req *http.Request, broker *broker.Shim, jobManifest *job.Manifest, conf *config.Config) {
	w.Header().Set("Content-Type", "application/json")

	brokerHealthy := (*broker).Healthy()
	uptime := time.Since(startupTime)

	jobManifest.Mutex.RLock()
	jobCount := jobManifest.Size()
	jobs := make(map[string]interface{})
	for id, job := range jobManifest.Jobs {
		jobs[id] = map[string]interface{}{
			"created": job.Created.Format(time.RFC3339),
			"age":     time.Since(job.Created).String(),
		}
	}
	jobManifest.Mutex.RUnlock()

	// Get detailed broker diagnostics
	brokerDetails := (*broker).GetHealthDetails()

	detailedStatus := map[string]interface{}{
		"status":         "healthy",
		"timestamp":      time.Now().Format(time.RFC3339),
		"uptime":         uptime.String(),
		"uptime_seconds": uptime.Seconds(),
		"ready":          isReady,
		"broker_type":    conf.BrokerType,
		"broker_healthy": brokerHealthy,
		"broker_details": brokerDetails,
		"jobs": map[string]interface{}{
			"count":           jobCount,
			"max_concurrency": conf.MaxConcurrency,
			"active_jobs":     jobs,
		},
		"config": map[string]interface{}{
			"worker_id":       conf.WorkerID,
			"broker_subject":  conf.BrokerSubject,
			"job_timeout":     conf.JobTimeout,
			"metrics_enabled": conf.MetricsEnabled,
		},
	}

	if !brokerHealthy {
		detailedStatus["status"] = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(detailedStatus)
}
