package main

import (
	"log"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hotellistat/AMPS/cmd/amps/app"
	"github.com/hotellistat/AMPS/cmd/amps/config"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	conf := config.New()

	if conf.SentryDsn != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn:         conf.SentryDsn,
			Environment: conf.Environment,
			Release:     "AMPS@" + conf.Version,
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
