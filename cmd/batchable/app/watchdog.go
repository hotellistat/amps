package app

import (
	"batchable/cmd/batchable/config"
	"sync"
	"time"

	"github.com/nats-io/stan.go"
)

// Watchdog is a goroutine that takes care of job timeouts and general state management
func Watchdog(
	conf *config.Config,
	jobManifest *JobManifest,
	broker *BrokerShim,
	manifestMutex *sync.Mutex) {
	go func() {
		for {
			jobManifest.Lock()

			jobManifest.DeleteDeceased(conf.JobTimeout)

			jobManifest.Unlock()

			if jobManifest.Size() < conf.MaxConcurrency {
				(*broker).Start(func(msg *stan.Msg) {
					MessageHandler(msg, conf, jobManifest, broker)
				})
			}

			sleepTime, _ := time.ParseDuration("100ms")
			time.Sleep(sleepTime)
		}
	}()
}
