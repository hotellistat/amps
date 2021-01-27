package app

import (
	"encoding/json"
	"errors"

	cloudevent "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
)

// UnmarshalCloudevent parses a cloudevent
func UnmarshalCloudevent(data []byte) (event.Event, error) {
	event := cloudevent.NewEvent()

	err := json.Unmarshal(data, &event)

	if err != nil {
		return cloudevent.NewEvent(), errors.New("Could not Marshal Cloud Event")
	}

	return event, nil
}
