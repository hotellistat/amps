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
	go func() {
		for {
			jobManifest.Lock()

			jobManifest.DeleteDeceased(conf.JobTimeout)

			jobManifest.Unlock()

			if jobManifest.Size() < conf.MaxConcurrency {
				(*broker).Start(jobManifest)
			}

			sleepTime, _ := time.ParseDuration("100ms")
			time.Sleep(sleepTime)
		}
	}()
}
