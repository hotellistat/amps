package broker

import (
	"batchable/cmd/batchable/config"
	"batchable/cmd/batchable/job"

	"github.com/cloudevents/sdk-go/v2/event"
)

type Shim interface {
	Initialize(config.Config)
	Teardown()
	Start(*job.Manifest)
	Stop()
	Healthy() bool
	PublishResult(config.Config, event.Event) error
}
