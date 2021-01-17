package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/nats-io/stan.go"
	broker "hotellistat.com/m/v2/src/lib"
)

// A Job represents one current workitem that needs to be processed
type Job struct {
	created time.Time
	message *stan.Msg
}

func timeoutWatchdog(brokerShim *broker.NATS, jobManifest map[uuid.UUID]Job, messageCallback stan.MsgHandler) {
	for {
		maxLifetime, _ := strconv.ParseInt(os.Getenv("JOB_TIMEOUT"), 10, 64)

		for id, job := range jobManifest {
			if (time.Now().Unix() - job.created.Unix()) > maxLifetime {
				log.Println("Job Timeout, ID:", id)
				delete(jobManifest, id)
			}
		}

		maxConcurrency, _ := strconv.ParseInt(os.Getenv("MAX_CONCURRENCY"), 10, 8)

		if int64(len(jobManifest)) <= maxConcurrency {
			brokerShim.Start(messageCallback)
		}

		time.Sleep(1 * time.Second)
	}
}

func main() {
	godotenv.Load()

	maxConcurrency, _ := strconv.ParseInt(os.Getenv("MAX_CONCURRENCY"), 10, 8)

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := make(map[uuid.UUID]Job)

	// Initialize a new broker instance, which is a general abstraction of the NATS go library
	brokerShim := broker.Initialize()

	// W need a mutext so that the manifest length check doesn't run into a race condition
	var mutex = &sync.Mutex{}

	insertJob := func(id uuid.UUID, msg *stan.Msg, waitgroup *sync.WaitGroup) {
		// Mutex takes care of the before mentioned race condtition
		// Create a new job and push it to the jobManifest
		jobManifest[id] = Job{
			created: time.Now(),
			message: msg,
		}

		mutex.Lock()

		if int64(len(jobManifest)) > maxConcurrency {
			brokerShim.Stop()
		}

		mutex.Unlock()

		waitgroup.Done()
	}

	triggerWorkload := func(id uuid.UUID, msg *stan.Msg, waitgroup *sync.WaitGroup) {
		values := map[string]string{
			"timeout": os.Getenv("timeout"),
			"id":      id.String(),
			"data":    string(msg.Data),
		}

		jsonData, err := json.Marshal(values)

		if err != nil {
			log.Fatal(err)
		}

		http.Post(os.Getenv("WORKLOAD_ADDRESS"), "application/json", bytes.NewBuffer(jsonData))
		waitgroup.Done()
	}

	// Our message handler which will do the main management of each message
	messageHandler := func(msg *stan.Msg) {
		defer msg.Ack()

		// Each Job recieves a UUID so we can target a specific job inside of this worker
		jobID, _ := uuid.NewRandom()

		var waitgroup sync.WaitGroup
		waitgroup.Add(2)

		go insertJob(jobID, msg, &waitgroup)
		go triggerWorkload(jobID, msg, &waitgroup)

		waitgroup.Wait()
	}

	// Create a new subscription for nats streaming
	brokerShim.Start(messageHandler)

	// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
	http.HandleFunc("/checkout", func(w http.ResponseWriter, req *http.Request) {
		type Body struct {
			ID   uuid.UUID
			data string
		}

		var d Body
		err := json.NewDecoder(req.Body).Decode(&d)
		if err != nil {
			w.WriteHeader(http.StatusNotAcceptable)
		}

		mutex.Lock()
		delete(jobManifest, d.ID)

		if int64(len(jobManifest)) <= maxConcurrency {
			// Initialize a new subscription should the old one have been closed
			brokerShim.Start(messageHandler)
		}
		mutex.Unlock()

	})

	go http.ListenAndServe(":4000", nil)

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if os.Getenv("JOB_TIMEOUT") != "false" {
		go timeoutWatchdog(&brokerShim, jobManifest, messageHandler)
	}

	// General signal handling to teardown the worker
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for range signalChan {
			fmt.Printf("\nReceived an interrupt, closing connection...\n\n")
			brokerShim.Teardown()

			cleanupDone <- true
		}
	}()
	<-cleanupDone

}
