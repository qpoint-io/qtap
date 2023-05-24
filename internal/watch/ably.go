package watch

import (
	"context"
	"fmt"

	"github.com/ably/ably-go/ably"
)

type Ably struct {
	QpointID string
	Token    string

	// internal
	chVersion   chan string
	client      *ably.Realtime
	unsubscribe func()
}

func (a *Ably) Watch() (chan string, error) {
	// initialize the channel for sending version updates
	a.chVersion = make(chan string)

	// connect to realtime
	client, err := ably.NewRealtime(ably.WithKey(a.Token))
	if err != nil {
		return nil, fmt.Errorf("connecting to ably realtime: %w", err)
	}

	// connect to the channel
	channel := client.Channels.Get(fmt.Sprintf("qtap-%s", a.QpointID))

	// listen for version updates
	unsubscribe, err := channel.Subscribe(context.Background(), "version", func(msg *ably.Message) {
		version, ok := msg.Data.(string)
		if ok {
			a.chVersion <- version
		}
	})
	if err != nil {
		return nil, fmt.Errorf("subscribing to realtime channel: %w", err)
	}

	// assign to struct
	a.client = client
	a.unsubscribe = unsubscribe

	return a.chVersion, nil
}

func (a *Ably) Stop() error {
	// unsubscribe
	a.unsubscribe()

	// disconnect
	a.client.Close()

	return nil
}
