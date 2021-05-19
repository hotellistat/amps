package main

import (
	"batchable/cmd/batchable/app"
	"batchable/cmd/batchable/config"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	conf := config.New()

	if conf.SentryDsn != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn:         conf.SentryDsn,
			Environment: conf.Environment,
			Release:     "batchable@" + conf.Version,
			Debug:       true,
		})

		if err != nil {
			log.Fatalf("sentry.Init: %s", err)
		}
	}

	defer sentry.Flush(2 * time.Second)
	defer sentry.Recover()

	app.Run(conf)
}
