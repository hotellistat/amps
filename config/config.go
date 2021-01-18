package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Config represents the global configuation for this project
type Config struct {
	NatsHost           string
	NatsCluster        string
	WorkerID           string
	BrokerType         string
	BrokerSubject      string
	BrokerDurableGroup string
	BrokerQueueGroup   string
	JobTimeout         int
	MaxConcurrency     int
	WorkloadAddress    string
}

// New returns a new Config struct
func New() *Config {

	workerID, _ := os.Hostname()

	return &Config{
		NatsHost:           getEnv("BATCHABLE_NATS_HOST", "localhost:4223"),
		NatsCluster:        getEnv("BATCHABLE_NATS_CLUSTER", "nats-cluster"),
		WorkerID:           getEnv("BATCHABLE_WORKER_ID", workerID),
		BrokerType:         getEnv("BATCHABLE_BROKER_TYPE", "nats"),
		BrokerSubject:      getEnvRequired("BATCHABLE_BROKER_SUBJECT"),
		BrokerDurableGroup: getEnv("BATCHABLE_BROKER_DURABLE_GROUP", ""),
		BrokerQueueGroup:   getEnv("BATCHABLE_BROKER_QUEUE_GROUP", ""),
		JobTimeout:         getEnvAsInt("BATCHABLE_JOB_TIMEOUT", 120),
		MaxConcurrency:     getEnvAsInt("BATCHABLE_MAX_CONCURRENCY", 100),
		WorkloadAddress:    getEnv("BATCHABLE_WORKLOAD_ADDRESS", "http://localhost:5050"),
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
