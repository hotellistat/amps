package test

import (
	"strings"
	"testing"

	"github.com/hotellistat/AMPS/cmd/amps/cloudevent"
	"github.com/joho/godotenv"
)

func TestUnmarshalValidSchema(t *testing.T) {
	godotenv.Load()

	validCloudevent := []byte(strings.ReplaceAll(
		`{
			"specversion": "1.0",
			"type": "event-type",
			"id": "someID",
			"source": "testing",
			"data": {
				"key": "value"
			}
		}`,
		"\n", ""))

	event, unmarshalError := cloudevent.Unmarshal(validCloudevent)

	if unmarshalError != nil {
		t.Fatal(unmarshalError.Error())
	}

	if event.ID() != "someID" {
		t.Fatal("Unmarshaling unsuccessful")
	}

	eventData := string(event.Data())

	if !strings.Contains(eventData, "key") || !strings.Contains(eventData, "value") {
		t.Fatal("Unmarshaling event data unsuccessful")
	}
}

func TestUnmarshalInvalidSchema(t *testing.T) {
	godotenv.Load()

	validCloudevent := []byte(
		`{
			"specversion": "1.0",
			"type": "event-type",
			"id": "someID",
			"source": "testing",
			"data": {
				"key": "value"`,
	)

	_, unmarshalError := cloudevent.Unmarshal(validCloudevent)

	if unmarshalError == nil {
		t.Fatal("Unmarshaling invalid json")
	}
}

func TestUnmarshalInvalidCloudevent(t *testing.T) {
	godotenv.Load()

	validCloudevent := []byte(
		`{
			"type": "event-type",
			"source": "testing",
			"data": {
				"key": "value"
			}
		}`,
	)

	_, unmarshalError := cloudevent.Unmarshal(validCloudevent)

	if unmarshalError == nil {
		t.Fatal("Unmarshaling invalid CloudEvent")
	}
}
