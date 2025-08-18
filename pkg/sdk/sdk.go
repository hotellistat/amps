package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	amqp "github.com/streadway/amqp"
)

type RejectValues struct {
	Identifier string
}

type AcknowledgeValues struct {
	Identifier string
}

type CleanupFcnType func(f interface{}) error

var CleanupFcn CleanupFcnType
var CleanupArg interface{}

//var AmqpUrl = "amqp://guest:guest@localhost:5672/"
//var PostUrl = "http://localhost"
var RunWithDebug bool = false
var RequesterName string = "UKNOWN"

func PublishRabbitmqNextStep(amqpUrl string, myName string, event cloudevents.Event) error {
	var err error = nil
	var conn *amqp.Connection
	var ch *amqp.Channel
	var q amqp.Queue

	if conn, err = amqp.Dial(amqpUrl); err != nil {
		log.Printf("ERROR in Dial: %v\n", err)
	}
	defer conn.Close()

	// instance over the connection we have already~~`
	// established.`
	if ch, err = conn.Channel(); err != nil {
		log.Printf("ERROR in Channel: %v\n", err)
	}
	defer ch.Close()

	var eventBytes []byte
	if eventBytes, err = json.Marshal(event); err != nil {
		log.Printf("ERROR in Marshal(event): %v\n", err)
	}
	if err = ch.Publish(
		"",     // exchange
		q.Name, // routing key
		false,  // mandatory
		false,  // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        eventBytes,
		}); err != nil {
		log.Printf("ERROR in Publish: %v\n", err)
	}
	return err
}

func acknowledge(w http.ResponseWriter, r *http.Request) error {
	var err error = nil
	var body []byte
	var acknowledgeValues AcknowledgeValues

	log.SetPrefix(fmt.Sprintf("[%s consume-publish-rabbitmq acknowledge] ", RequesterName))
	if RunWithDebug {
		log.Printf("GOT ACKNOWLEDGE\n")
	}
	if body, err = ioutil.ReadAll(r.Body); err != nil {
		log.Printf("ERROR acknowledge ReadAll(r.Body): %v\n", err)
		return err
	}

	if len(body) > 0 {
		if err = json.Unmarshal(body, &acknowledgeValues); err != nil {
			log.Printf("ERROR master Unmarshal(body): %v\n", err)
		}
	}
	if RunWithDebug {
		log.Printf(">>> acknowledge body: %v\n", acknowledgeValues)
	}
	return err
}

func reject(w http.ResponseWriter, r *http.Request) error {
	var err error = nil
	var body []byte
	var rejectValues RejectValues

	log.SetPrefix(fmt.Sprintf("[%s consume-publish-rabbitmq reject] ", RequesterName))
	log.Printf("GOT reject\n")
	if body, err = ioutil.ReadAll(r.Body); err != nil {
		log.Printf("ERROR reject ReadAll(r.Body): %v\n", err)
		return err
	}

	if len(body) > 0 {
		if err = json.Unmarshal(body, &rejectValues); err != nil {
			log.Printf("ERROR Unmarshal(body): %v\n", err)
		}
	}
	if RunWithDebug {
		log.Printf(">>> reject body: %v\n", rejectValues)
	}
	return err
}

//func publish(w http.ResponseWriter, r *http.Request) error
func publishHandler(sentryHandler *sentryhttp.Handler, amqpUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error = nil
		var body []byte
		var event cloudevents.Event

		if r.Method != "POST" {
			fmt.Fprintf(w, "publishHandler Only POST is allowed")
			return
		}

		log.SetPrefix(fmt.Sprintf("[%s consume-publish-rabbitmq publish] ", RequesterName))
		if RunWithDebug {
			log.Printf("GOT publish\n")
		}
		if r.Method != "POST" {
			log.Printf("ERROR in publish only POST is allowed")
			return
		}
		if body, err = ioutil.ReadAll(r.Body); err != nil {
			log.Printf("ERROR publish ReadAll(r.Body): %v\n", err)
			return
		}

		if len(body) > 0 {
			if err = json.Unmarshal(body, &event); err != nil {
				log.Printf("ERROR publish Unmarshal(body): %v\n", err)
				return
			}
		}
		if err = PublishRabbitmqNextStep(amqpUrl, RequesterName, event); err != nil {
			log.Printf("ERROR in PublishRabbitmqNextStep: %v\n", err)
			return
		}
		return
	}
}

func consume(eventTypeFrom string, amqpUrl string, postUrl string, postPort string) error {
	var err error = nil
	var conn *amqp.Connection
	var ch *amqp.Channel
	var q amqp.Queue

	if RunWithDebug {
		log.Printf("=== ConsumePublish\n")
	}
	if conn, err = amqp.Dial(amqpUrl); err != nil {
		log.Printf("ERROR Failed to connect to RabbitMQ%v\n", err)
	}
	defer conn.Close()

	if ch, err = conn.Channel(); err != nil {
		log.Printf("ERROR Failed to open a channel %v\n", err)
	}
	defer ch.Close()

	if q, err = ch.QueueDeclare(
		eventTypeFrom, // name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	); err != nil {
		log.Printf("ERROR Failed to QueueDeclare %v\n", err)
	}

	var msgs <-chan amqp.Delivery
	if msgs, err = ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	); err != nil {
		log.Printf("ERROR Failed to register a consumer %v\n", err)
	}

	forever := make(chan bool)
	go func() {
		var resp *http.Response
		var url string
		var idx int = 0
		//var eventDataValues EventData
		var event cloudevents.Event

		for d := range msgs {
			idx++
			if RunWithDebug {
				log.Printf("=== idx: %d\n", idx)
				log.Printf("received a message: %s", d.Body)
			}
			url = fmt.Sprintf(postUrl+"%s", postPort)
			if err = json.Unmarshal(d.Body, &event); err != nil {
				log.Printf("ERROR in Unmarshal(d.Body) %v\n", err)
				return
			}
			log.Printf("Received: q.Name: %v\nevent: %v\n", q.Name, event)
			if resp, err = http.Post(url, "application/json", bytes.NewBuffer(d.Body)); err != nil {
				log.Printf("ERROR in consume sending to %s %v\n", url, err)
				return
			}
			if resp != nil && resp.StatusCode != http.StatusOK {
				log.Printf("ERROR resp.StatusCode %v\n", resp.StatusCode)
			}
			// make a delay as we do not have AMPS for avoiding
			// "too many open connections" in the receiving parts
			time.Sleep(1 * time.Second)
		}
	}()

	log.Printf("Waiting for messages. To exit press CTRL+C\n")
	<-forever
	return err
}

func publishServer(amqpUrl string, postUrl string, listenerPort string) {
	var err error = nil

	server := http.NewServeMux()
	sentryHandler := sentryhttp.New(sentryhttp.Options{})
	// This endpoint for debugging without using amps, where workloads can notify nats, that they have finished
	server.HandleFunc("/acknowledge", sentryHandler.HandleFunc(func(w http.ResponseWriter, req *http.Request) {
		localHub := sentry.GetHubFromContext(req.Context())

		if req.Method != "POST" {
			fmt.Fprintf(w, "Only POST is allowed")
			return
		}
		err = acknowledge(w, req)
		if err != nil {
			localHub.CaptureException(err)
		}
	}))
	// This endpoint handles job deletion
	server.HandleFunc("/reject", sentryHandler.HandleFunc(func(w http.ResponseWriter, req *http.Request) {
		localHub := sentry.GetHubFromContext(req.Context())
		if req.Method != "POST" {
			fmt.Fprintf(w, "Only POST is allowed")
			return
		}
		err = reject(w, req)
		if err != nil {
			localHub.CaptureException(err)
		}
	}))

	// This endpoint handles publish
	server.HandleFunc("/publish", publishHandler(sentryHandler, amqpUrl))
	if RunWithDebug {
		log.Printf("will listen on %s\n", listenerPort)
	}
	if err = http.ListenAndServe(listenerPort, server); err != nil {
		log.Fatalf("consume-publish-rabbitnq unable to start http server, %v", err)
	}
	return
}

func ConsumePublish(eventTypeFrom string, amqpUrl string, postUrl, postPort string, listenerPort string) error {
	var err error = nil

	log.SetPrefix(fmt.Sprintf("[%s] ", RequesterName))
	go consume(eventTypeFrom, amqpUrl, postUrl, postPort)
	go publishServer(amqpUrl, postUrl, listenerPort)

	// General signal handling to teardown the worker
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for signal := range signalChan {
			log.Printf("signal: %s", signal.String())
			if CleanupFcn != nil {
				if err = CleanupFcn(CleanupArg); err != nil {
					log.Printf("ERROR in CleanupFcn: %v", err)
				}
			}
			cleanupDone <- true
		}
	}()
	<-cleanupDone

	return err
}
