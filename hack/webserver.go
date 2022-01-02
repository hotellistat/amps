package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"

	"net/http"
	"time"

	"github.com/hotellistat/AMPS/cmd/amps/cloudevent"
)

func main() {
	webserver := http.NewServeMux()

	webserver.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))

		body, _ := ioutil.ReadAll(req.Body)

		event, _ := cloudevent.Unmarshal(body)

		eventID := event.ID()

		println("New job", eventID)

		go func() {
			time.Sleep(1 * time.Second)
			client := http.Client{
				Timeout: 30 * time.Second,
			}

			println("Sending ack")

			type requestBody struct {
				Identifier string `json:"identifier"`
				Reschedule bool   `json:"reschedule"`
			}

			reqBodyInstance, _ := json.Marshal(requestBody{eventID, true})

			client.Post("http://localhost:4000/acknowledge", "application/json", bytes.NewBuffer(reqBodyInstance))
		}()
	})

	webserver.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.ListenAndServe(":8000", webserver)
}
