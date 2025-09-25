package app

import (
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hotellistat/amps/cmd/amps/broker"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
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
				println("[AMPS] Job ID:", ID, "timed out")
				jobManifest.DeleteJob(ID)
			}
		}

		jobManifest.Mutex.Unlock()
	}
}
