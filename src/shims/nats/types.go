package natsshim

import (
	"github.com/nats-io/nats.go"
	"github.com/nats-io/stan.go"
)

// NATS represents the primary NATS communication instance
type NATS struct {
	NatsConnection *nats.Conn
	StanConnection stan.Conn
	Subscription   stan.Subscription
}
