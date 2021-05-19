package app

import (
	"batchable/cmd/batchable/broker"
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// Watchdog is a goroutine that takes care of job timeouts and general state management
func Watchdog(
	ticker *time.Ticker,
	conf *config.Config,
	jobManifest *job.Manifest,
	broker *broker.Shim,
	manifestMutex *sync.Mutex,
) {
	localHub := sentry.CurrentHub().Clone()
	localHub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("goroutine", "watchdog")
	})

	for range ticker.C {
		jobManifest.Mutex.Lock()
		for ID, jobItem := range jobManifest.Jobs {
			if time.Since(jobItem.Created) > conf.JobTimeout {
				println("[batchable] Job ID:", ID, "timed out")
				jobManifest.DeleteJob(ID)
			}
		}

		startBroker := jobManifest.Size() < conf.MaxConcurrency
		jobManifest.Mutex.Unlock()

		if startBroker {
			err := (*broker).Start()
			if err != nil {
				localHub.CaptureException(err)
			}
		}
	}
}
