package main

import (
	"batchable/internal/config"
	"batchable/internal/kafkashim"
	"batchable/internal/natsshim"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/joho/godotenv"
	"github.com/nats-io/stan.go"
)

// A Job represents one current workitem that needs to be processed
type Job struct {
	created time.Time
	message *stan.Msg
}

// BrokerShim is an abstracion of the functions that each broker shim needs to implement
type BrokerShim interface {
	Initialize(config.Config)
	Teardown()
	Start(stan.MsgHandler)
	Stop()
	PublishResult(config.Config, event.Event) error
}

func insertJob(
	event cloudevents.Event,
	jobManifest map[string]Job,
	broker *BrokerShim,
	conf config.Config,
	mutex *sync.Mutex) error {
	// Mutex takes care of the before mentioned race condtition
	mutex.Lock()

	eventID := event.Context.GetID()

	_, isDuplicate := jobManifest[eventID]
	if isDuplicate {
		mutex.Unlock()
		return errors.New("A Job with this ID already exists")
	}
	// Create a new job and push it to the jobManifest
	jobManifest[eventID] = Job{
		created: time.Now(),
	}

	if int(len(jobManifest)) >= conf.MaxConcurrency {
		if conf.Debug {
			log.Println("Max job concurrency reached, stopping broker")
		}
		(*broker).Stop()
	}

	mutex.Unlock()
	return nil
}

func main() {
	godotenv.Load()

	conf := config.New()

	brokerTypes := map[string]BrokerShim{
		"nats":  &natsshim.NatsBroker{},
		"kafka": &kafkashim.KafkaBroker{},
	}

	broker, ok := brokerTypes[conf.BrokerType]

	if !ok {
		log.Fatal("Could not find broker type", conf.BrokerType)
	}

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := make(map[string]Job)

	// Initialize a new broker instance, which is a general abstraction of the NATS go library
	broker.Initialize(*conf)

	triggerWorkload := func(event cloudevents.Event, waitgroup *sync.WaitGroup) {

		eventData, err := json.Marshal(event)

		if err != nil {
			log.Println("Could not marshal cloudevent for workload")
			return
		}

		client := http.Client{
			Timeout: 20 * time.Second,
		}

		resp, err := client.Post(conf.WorkloadAddress, "application/json", bytes.NewBuffer(eventData))
		if err == nil {
			body, _ := ioutil.ReadAll(resp.Body)
			println("response", string(body))
		}

		waitgroup.Done()
	}

	var mutex = &sync.Mutex{}

	// Our message handler which will do the main management of each message
	messageHandler := func(msg *stan.Msg) {
		msg.Ack()

		event := cloudevents.NewEvent()

		err := json.Unmarshal(msg.Data, &event)

		if err != nil {
			log.Println("Could not Marshal Cloud Event")
			return
		}

		if conf.Debug {
			log.Println("Job ID:", event.Context.GetID(), "Data:", string(msg.Data))
		}

		var waitgroup sync.WaitGroup
		waitgroup.Add(1)

		go triggerWorkload(event, &waitgroup)

		insertErr := insertJob(event, jobManifest, &broker, *conf, mutex)
		if insertErr != nil {
			log.Println(insertErr.Error())
		}

		waitgroup.Wait()
	}

	// Create a new subscription for nats streaming
	broker.Start(messageHandler)

	// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
	http.HandleFunc("/checkout", func(w http.ResponseWriter, req *http.Request) {

		cloudevent := cloudevents.NewEvent()

		body, readErr := ioutil.ReadAll(req.Body)

		if readErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Could not read request Body"))
			return
		}

		ceErr := json.Unmarshal(body, &cloudevent)

		if ceErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Can not unmarshal cloudevent, make sure you send a cloudevent in structured content mode"))
			return
		}

		// Fetch the nopublish event context extension. This will prevent publishing the recieved event to our broker.
		// This is normally used, if you want to define the end of a chain of workloads, where the last link of the chain
		// Should not create any new events in the broker anymore
		data, _ := cloudevent.Context.GetExtension("nopublish")

		jobID := cloudevent.Context.GetID()

		if conf.Debug {
			println("Deleting Job ID:", jobID)
		}

		if data != true {
			publishErr := broker.PublishResult(*conf, cloudevent)
			if publishErr != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Could not publish your event to the broker"))
				mutex.Unlock()
				return
			}
		}

		mutex.Lock()
		delete(jobManifest, jobID)

		if int(len(jobManifest)) < conf.MaxConcurrency {
			// Initialize a new subscription should the old one have been closed
			broker.Start(messageHandler)
		}
		mutex.Unlock()

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("OK"))
	})

	go http.ListenAndServe(":4000", nil)

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if conf.JobTimeout != 0 {
		go func() {
			for {
				maxLifetime := conf.JobTimeout

				for id, job := range jobManifest {
					if int(time.Now().Unix()-job.created.Unix()) > maxLifetime {
						if conf.Debug {
							log.Println("Job Timeout, ID:", id)
						}
						delete(jobManifest, id)
					}
				}

				if int(len(jobManifest)) < conf.MaxConcurrency {
					broker.Start(messageHandler)
				}

				time.Sleep(1 * time.Second)
			}
		}()
	}

	// General signal handling to teardown the worker
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for range signalChan {
			fmt.Printf("\nReceived an interrupt, closing connection...\n\n")
			broker.Teardown()

			cleanupDone <- true
		}
	}()
	<-cleanupDone

}
