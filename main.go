package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"plugin"
	"strconv"
	"sync"
	"time"

	"batchable/config"

	"github.com/google/uuid"
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
}

func main() {
	godotenv.Load()

	conf := config.New()

	brokerType := "nats"

	var mod string

	path, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}

	switch brokerType {
	case "nats":
		mod = filepath.Join(path, "shims/nats/shim.so")
	default:
		fmt.Println("Broker type not supported")
		os.Exit(1)
	}

	plug, err := plugin.Open(mod)
	if err != nil {
		fmt.Println("yho", err)
		os.Exit(1)
	}

	symBrokerShim, err := plug.Lookup("BrokerShimExport")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	broker := symBrokerShim.(BrokerShim)

	maxConcurrency := conf.MaxConcurrency

	// Initialize our job manifest. This will hold all currently active jobs for this worker
	jobManifest := make(map[uuid.UUID]Job)

	// Initialize a new broker instance, which is a general abstraction of the NATS go library
	broker.Initialize(*conf)

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

		if int(len(jobManifest)) >= maxConcurrency {
			broker.Stop()
		}

		mutex.Unlock()
		waitgroup.Done()
	}

	triggerWorkload := func(id uuid.UUID, msg *stan.Msg) {
		values := map[string]string{
			"timeout": strconv.Itoa(conf.JobTimeout),
			"id":      id.String(),
			"data":    string(msg.Data),
		}

		jsonData, err := json.Marshal(values)

		if err != nil {
			log.Fatal(err)
		}

		client := http.Client{
			Timeout: 20 * time.Second,
		}

		resp, err := client.Post(conf.WorkloadAddress, "application/json", bytes.NewBuffer(jsonData))
		if err == nil {
			body, _ := ioutil.ReadAll(resp.Body)
			println("response", string(body))
		}
	}

	// Our message handler which will do the main management of each message
	messageHandler := func(msg *stan.Msg) {
		msg.Ack()
		log.Println(string(msg.Data))
		// Each Job recieves a UUID so we can target a specific job inside of this worker
		jobID, _ := uuid.NewRandom()

		var waitgroup sync.WaitGroup
		waitgroup.Add(1)

		go insertJob(jobID, msg, &waitgroup)
		go triggerWorkload(jobID, msg)

		waitgroup.Wait()
	}

	// Create a new subscription for nats streaming
	broker.Start(messageHandler)

	// This endpoint is the checkout endpoint, where workloads can notify nats, that they have finished
	http.HandleFunc("/checkout", func(w http.ResponseWriter, req *http.Request) {
		type Body struct {
			ID string
		}

		var d Body
		err := json.NewDecoder(req.Body).Decode(&d)
		if err != nil {
			w.WriteHeader(http.StatusNotAcceptable)
		}

		println("Deleting Job ID:", d.ID)
		mutex.Lock()
		parsedJobID, err := uuid.Parse(d.ID)

		if err != nil {
			return
		}

		delete(jobManifest, parsedJobID)

		if int(len(jobManifest)) < maxConcurrency {
			// Initialize a new subscription should the old one have been closed
			broker.Start(messageHandler)
		}
		mutex.Unlock()

	})

	go http.ListenAndServe(":4000", nil)

	// The watchdog, if enabled, checks the timeout of each Job and deletes it if it got too old
	if conf.JobTimeout != 0 {
		go func() {
			for {
				maxLifetime := conf.JobTimeout

				for id, job := range jobManifest {
					if int(time.Now().Unix()-job.created.Unix()) > maxLifetime {
						log.Println("Job Timeout, ID:", id)
						delete(jobManifest, id)
					}
				}

				if int(len(jobManifest)) < maxConcurrency {
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
