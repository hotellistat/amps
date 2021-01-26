package main

import (
	"batchable/internal/config"
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

func main() {
	godotenv.Load()

	conf := config.New()

	brokerTypes := map[string]BrokerShim{
		"nats": &natsshim.NatsBroker{},
	}

	broker, ok := brokerTypes[conf.BrokerType]

	if !ok {
		log.Fatal("Could not find broker type:", conf.BrokerType)
	}

	var manifestMutex = &sync.Mutex{}

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := make(map[string]Job)

	// Initialize a new broker instance.
	broker.Initialize(*conf)

	// Create a new subscription for nats streaming
	broker.Start(func(msg *stan.Msg) {
		messageHandler(msg, conf, jobManifest, &broker, manifestMutex)
	})

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if conf.JobTimeout != 0 {
		go watchdog(conf, jobManifest, &broker, manifestMutex)
	}

	// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
	http.HandleFunc("/checkout", func(w http.ResponseWriter, req *http.Request) {
		jobCheckout(w, req, conf, jobManifest, &broker, manifestMutex)
	})

	go http.ListenAndServe(":4000", nil)

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

func messageHandler(
	msg *stan.Msg,
	conf *config.Config,
	jobManifest map[string]Job,
	broker *BrokerShim,
	manifestMutex *sync.Mutex) {

	event := cloudevents.NewEvent()

	err := json.Unmarshal(msg.Data, &event)

	if err != nil {
		log.Println("Could not Marshal Cloud Event", string(msg.Data))
		msg.Ack()
		log.Println(err.Error())
		return
	}

	if conf.Debug {
		log.Println("Job ID:", event.Context.GetID())
	}

	insertJobErr := make(chan error, 1)

	insertJob(msg, event, jobManifest, broker, *conf, manifestMutex, insertJobErr)

	insertJobErrResult := <-insertJobErr

	if insertJobErrResult != nil {
		log.Println(insertJobErrResult)
		return
	}

	workloadErr := make(chan error, 1)

	triggerWorkload(event, *conf, workloadErr)

	workloadErrResult := <-workloadErr

	if workloadErrResult != nil {
		log.Println(workloadErrResult)
	}

}

func insertJob(
	msg *stan.Msg,
	event cloudevents.Event,
	jobManifest map[string]Job,
	broker *BrokerShim,
	conf config.Config,
	manifestMutex *sync.Mutex,
	ch chan<- error) {
	// Mutex takes care of the before mentioned race condtition
	(*manifestMutex).Lock()

	eventID := event.Context.GetID()

	_, isDuplicate := jobManifest[eventID]
	if isDuplicate {
		(*manifestMutex).Unlock()
		ch <- errors.New("Job ID: " + eventID + " this job already exists")
		close(ch)
		return
	}

	// Create a new job and push it to the jobManifest
	jobManifest[eventID] = Job{
		created: time.Now(),
	}

	ackErr := msg.Ack()
	if ackErr != nil {
		log.Println("Could not Acknowledge job:", ackErr.Error())
	}

	// Stop broker from recieving any more jobs after maxConcurrency is reached
	if len(jobManifest) >= conf.MaxConcurrency {
		if conf.Debug {
			log.Println("Max job concurrency reached, stopping broker")
		}
		(*broker).Stop()
	}

	(*manifestMutex).Unlock()

	ch <- nil
	close(ch)
}

func triggerWorkload(
	event cloudevents.Event,
	conf config.Config,
	ch chan<- error) {

	eventData, err := json.Marshal(event)

	if err != nil {
		ch <- errors.New("Could not marshal cloudevent for workload")
		close(ch)
		return
	}

	client := http.Client{
		Timeout: conf.WorkloadResponseTimeout,
	}

	resp, err := client.Post(conf.WorkloadAddress, "application/json", bytes.NewBuffer(eventData))
	if err == nil {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Println("response", string(body))
	}

	ch <- nil
	close(ch)
}

func jobCheckout(
	w http.ResponseWriter,
	req *http.Request,
	conf *config.Config,
	jobManifest map[string]Job,
	broker *BrokerShim,
	manifestMutex *sync.Mutex) {

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

	eventID := cloudevent.Context.GetID()
	_, exists := jobManifest[eventID]
	if !exists {
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("Could not publish your event to the broker. Job may have timed out."))
		log.Println("Job ID:", eventID, "does not exists anymore. Publishing blocked.")
		return
	}

	// Fetch the nopublish event context extension. This will prevent publishing the recieved event to our broker.
	// This is normally used, if you want to define the end of a chain of workloads, where the last link of the chain
	// Should not create any new events in the broker anymore
	data, _ := cloudevent.Context.GetExtension("nopublish")

	jobID := cloudevent.Context.GetID()

	if conf.Debug {
		log.Println("Deleting Job ID:", jobID)
	}

	if data != true {
		if conf.Debug {
			log.Println("Publishing recieved event to broker")
		}
		publishErr := (*broker).PublishResult(*conf, cloudevent)
		if publishErr != nil {
			log.Println("Could not publish event to broker")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Could not publish your event to the broker"))
			return
		}
	}

	(*manifestMutex).Lock()
	delete(jobManifest, jobID)
	if len(jobManifest) < conf.MaxConcurrency {
		// Initialize a new subscription should the old one have been closed
		(*broker).Start(func(msg *stan.Msg) {
			messageHandler(msg, conf, jobManifest, broker, manifestMutex)
		})
	}
	(*manifestMutex).Unlock()

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("OK"))
}

func watchdog(
	conf *config.Config,
	jobManifest map[string]Job,
	broker *BrokerShim,
	manifestMutex *sync.Mutex) {
	go func() {

		maxLifetime := conf.JobTimeout

		for {
			(*manifestMutex).Lock()
			for id, job := range jobManifest {
				if time.Now().Sub(job.created) > maxLifetime {
					if conf.Debug {
						log.Println("Job ID:", id, "timed out")
					}
					delete(jobManifest, id)
				}
			}

			if len(jobManifest) < conf.MaxConcurrency {
				(*broker).Start(func(msg *stan.Msg) {
					messageHandler(msg, conf, jobManifest, broker, manifestMutex)
				})
			}
			(*manifestMutex).Unlock()

			sleepTime, _ := time.ParseDuration("100ms")
			time.Sleep(sleepTime)
		}
	}()
}
