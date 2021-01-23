package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config represents the global configuation for this project
type Config struct {
	NatsHost                string
	NatsCluster             string
	WorkerID                string
	BrokerType              string
	BrokerSubject           string
	BrokerResultSubject     string
	BrokerDurableGroup      string
	BrokerQueueGroup        string
	Debug                   bool
	JobTimeout              time.Duration
	MaxConcurrency          int
	WorkloadAddress         string
	WorkloadResponseTimeout time.Duration
}

// New returns a new Config struct
func New() *Config {

	workerID, _ := os.Hostname()

	return &Config{
		NatsHost:                getEnv("NATS_HOST", "localhost:4223"),
		NatsCluster:             getEnv("NATS_CLUSTER", "nats-cluster"),
		WorkerID:                getEnv("WORKER_ID", workerID),
		BrokerType:              getEnv("BROKER_TYPE", "nats"),
		BrokerSubject:           getEnvRequired("BROKER_SUBJECT"),
		BrokerResultSubject:     getEnv("BROKER_RESULT_SUBJECT", ""),
		BrokerDurableGroup:      getEnv("BROKER_DURABLE_GROUP", ""),
		BrokerQueueGroup:        getEnv("BROKER_QUEUE_GROUP", ""),
		Debug:                   getEnv("DEBUG", "") == "true",
		MaxConcurrency:          getEnvAsInt("MAX_CONCURRENCY", 100),
		JobTimeout:              getEnvAsTime("JOB_TIMEOUT", "2m"),
		WorkloadResponseTimeout: getEnvAsTime("WORKLOAD_RESPONSE_TIMEOUT", "10s"),
		WorkloadAddress:         getEnv("WORKLOAD_ADDRESS", "http://localhost:5050"),
	}
}

// Simple helper function to read an environment or return a default value
func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}

func getEnvRequired(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatal("The environment variable: '", key, "' is required and has to be set")
		os.Exit(1)
	}

	return value
}

// Simple helper function to read an environment variable into integer or return a default value
func getEnvAsInt(name string, defaultVal int) int {
	valueStr := getEnv(name, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}

	return defaultVal
}

// Simple helper function to read an environment variable into integer or return a default value
func getEnvAsTime(name string, defaultVal string) time.Duration {
	valueStr := getEnv(name, "")
	if value, err := time.ParseDuration(valueStr); err == nil {
		return value
	}

	defaultDuration, _ := time.ParseDuration(defaultVal)

	return defaultDuration
}

// Helper to read an environment variable into a bool or return default value
func getEnvAsBool(name string, defaultVal bool) bool {
	valStr := getEnv(name, "")
	if val, err := strconv.ParseBool(valStr); err == nil {
		return val
	}

	return defaultVal
}

// Helper to read an environment variable into a string slice or return default value
func getEnvAsSlice(name string, defaultVal []string, sep string) []string {
	valStr := getEnv(name, "")

	if valStr == "" {
		return defaultVal
	}

	val := strings.Split(valStr, sep)

	return val
}
