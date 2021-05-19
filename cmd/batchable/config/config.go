package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type exitCallback func(code int)

// Config represents the global configuation for this project
type Config struct {
	Version                 string        `description:"The batchable container version"`
	BrokerType              string        `description:"The broker type you want to use, currently only 'amqp' is supported"`
	BrokerDsn               string        `description:"The brokers' connection DSN. This should hold the broker endpoint aswell as authentication values"`
	BrokerSubject           string        `description:"The broker subject on which batchable container should listen to"`
	WorkerID                string        `description:"The workers' ID. This has to be unique across all your batchable containers"`
	Port                    int           `description:"The port on which the batchable container HTTP server should be reachable"`
	Debug                   bool          `description:"Enable debug mode for verbose output"`
	SentryDsn               string        `description:"The Sentry DSN enpoint to send error logs to"`
	Environment             string        `description:"The in which the batchable container is running"`
	MetricsEnabled          bool          `description:"Enable the prometheus metrics exporter"`
	MetricsPort             int           `description:"The prometheus metrics exported port"`
	MaxConcurrency          int           `description:"The maximum amount of jobs that can run concurrently"`
	JobTimeout              time.Duration `description:"The maximum lifetime of a job. If this is exceeded, a job will be deleted from the queue"`
	WorkloadAddress         string        `description:"The workload address to which to send the job payload"`
	WorkloadResponseTimeout time.Duration `description:"The wokrload response timeout, after which the job will be returned to the broker"`
}

// New returns a new Config struct
func New() *Config {

	workerID, _ := os.Hostname()

	return &Config{
		Version:                 GetEnv("BATCHABLE_VERSION", "master"),
		BrokerType:              GetEnv("BROKER_TYPE", "amqp"),
		BrokerDsn:               GetEnv("BROKER_HOST", "amqp://localhost:5672"),
		BrokerSubject:           GetEnvRequired("BROKER_SUBJECT", func(code int) { os.Exit(code) }),
		WorkerID:                GetEnv("WORKER_ID", workerID),
		Port:                    GetEnvAsInt("PORT", 4000),
		Debug:                   GetEnvAsBool("DEBUG", false),
		SentryDsn:               GetEnv("SENTRY_DSN", ""),
		Environment:             GetEnv("ENVIRONMENT", "development"),
		MetricsEnabled:          GetEnvAsBool("METRICS_ENABLED", true),
		MetricsPort:             GetEnvAsInt("METRICS_PORT", 9090),
		MaxConcurrency:          GetEnvAsInt("MAX_CONCURRENCY", 100),
		JobTimeout:              GetEnvAsDuration("JOB_TIMEOUT", "2m"),
		WorkloadAddress:         GetEnv("WORKLOAD_ADDRESS", "http://localhost:5050"),
		WorkloadResponseTimeout: GetEnvAsDuration("WORKLOAD_RESPONSE_TIMEOUT", "30s"),
	}
}

// GetEnv is a simple helper function to read an environment or return a default value
func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}

// GetEnvRequired thrwos an error, should a env value not be found
func GetEnvRequired(key string, exit exitCallback) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		println("[batchable] The environment variable:", key, "is required and has to be set")
		exit(1)
	}

	return value
}

// GetEnvAsInt fetches a env as integer
func GetEnvAsInt(name string, defaultVal int) int {
	valueStr := GetEnv(name, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}

	return defaultVal
}

// GetEnvAsDuration fetches a env as a time.Duration
func GetEnvAsDuration(name string, defaultVal string) time.Duration {
	valueStr := GetEnv(name, "")
	if value, err := time.ParseDuration(valueStr); err == nil {
		return value
	}

	defaultDuration, _ := time.ParseDuration(defaultVal)

	return defaultDuration
}

// GetEnvAsBool fetches a env and parses the bool
func GetEnvAsBool(name string, defaultVal bool) bool {
	valStr := GetEnv(name, "")
	if val, err := strconv.ParseBool(valStr); err == nil {
		return val
	}

	return defaultVal
}

// GetEnvAsSlice recieves a delimited array and returns a parsed array
func GetEnvAsSlice(name string, defaultVal []string, sep string) []string {
	valStr := GetEnv(name, "")

	if valStr == "" {
		return defaultVal
	}

	val := strings.Split(valStr, sep)

	return val
}
