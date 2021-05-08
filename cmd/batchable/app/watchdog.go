package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"sync"
	"time"
)

// Watchdog is a goroutine that takes care of job timeouts and general state management
func Watchdog(
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim,
	manifestMutex *sync.Mutex) {
	tickInterval, _ := time.ParseDuration("1000ms")
	ticker := time.NewTicker(tickInterval)
	go func() {
		for range ticker.C {
			jobManifest.Mutex.Lock()
			jobManifest.DeleteDeceased(conf.JobTimeout)
			startBroker := jobManifest.Size() < conf.MaxConcurrency
			jobManifest.Mutex.Unlock()

			if startBroker {
				(*broker).Start()
			}
		}
	}()
}
