package broker

import (
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/hotellistat/amps/cmd/amps/config"
	"github.com/hotellistat/amps/cmd/amps/job"
)

type Shim interface {

	// Initial setup for all broker connections and runtime state
	Initialize(config.Config, *job.Manifest) bool

	// Gracefully disconnect broker from any external resources
	Teardown()

	// Start the broker to enable message recieving
	Start() error

	// Rreturn true if the broker is currently active (if Start was called)
	IsRunning() bool

	// Stop message recieving, while still keeping open fundamental connections to external resources
	Stop() error

	// Healthcheck to verify that the broker is not stuck. This will restart the container if it fails and healthchecks are set up correctly
	Healthy() bool

	// Get detailed health information for monitoring and debugging
	GetHealthDetails() map[string]interface{}

	// Publish new message to broker. This is used for chaining events.
	PublishMessage(event.Event) error
}
