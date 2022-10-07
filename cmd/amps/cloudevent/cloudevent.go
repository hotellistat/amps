package cloudevent

import (
	"encoding/json"
	"errors"
	"fmt"

	cloudevent "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/event"
)

// UnmarshalCloudevent parses a cloudevent
func Unmarshal(data []byte) (event.Event, error) {
	event := cloudevent.NewEvent()

	fmt.Println(string(data))
	err := json.Unmarshal(data, &event)

	if err != nil {
		fmt.Println(err)
		return cloudevent.NewEvent(), errors.New("could not Unmarshal Cloud Event")
	}

	return event, nil
}
