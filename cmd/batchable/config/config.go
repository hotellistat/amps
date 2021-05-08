package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type exitCallback func(code int)

// Config represents the global configuation for this project
type Config struct {
	BrokerUsername          string
	BrokerPassword          string
	BrokerHost              string
	BrokerCluster           string
	WorkerID                string
	BrokerType              string
	BrokerSubject           string
	Debug                   bool
	InstantAck              bool
	RejectZombieJobs        bool
	JobTimeout              time.Duration
	MaxConcurrency          int
	WorkloadAddress         string
	WorkloadResponseTimeout time.Duration
}

// New returns a new Config struct
func New() *Config {

	workerID, _ := os.Hostname()

	return &Config{
		BrokerUsername:          GetEnv("BROKER_AUTH_USERNAME", ""),
		BrokerPassword:          GetEnv("BROKER_AUTH_PASSWORD", ""),
		BrokerHost:              GetEnv("BROKER_HOST", "localhost"),
		BrokerCluster:           GetEnv("BROKER_CLUSTER", "nats-cluster"),
		WorkerID:                GetEnv("WORKER_ID", workerID),
		BrokerType:              GetEnv("BROKER_TYPE", "nats"),
		BrokerSubject:           GetEnvRequired("BROKER_SUBJECT", func(code int) { os.Exit(code) }),
		Debug:                   GetEnvAsBool("DEBUG", false),
		InstantAck:              GetEnvAsBool("INSTANT_ACK", false),
		RejectZombieJobs:        GetEnvAsBool("REJECT_ZOMBIE_JOBS", true),
		MaxConcurrency:          GetEnvAsInt("MAX_CONCURRENCY", 100),
		JobTimeout:              GetEnvAsDuration("JOB_TIMEOUT", "2m"),
		WorkloadResponseTimeout: GetEnvAsDuration("WORKLOAD_RESPONSE_TIMEOUT", "30s"),
		WorkloadAddress:         GetEnv("WORKLOAD_ADDRESS", "http://localhost:5050"),
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
		log.Println("[batchable] The environment variable: '", key, "' is required and has to be set")
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
