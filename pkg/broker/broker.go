package broker

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
)

const (
	defaultBufferSize = 512
)

type Event struct {
	Topic string
	TS    time.Time
	Data  any
}

type BrokerOpt func(*Broker)

func SetBufferSize(bufferSize int) BrokerOpt {
	return func(b *Broker) {
		b.bufferSize = bufferSize
	}
}

type Broker struct {
	bufferSize int

	logger  *zap.Logger
	subs    map[chan<- *Event]*subscriber
	stopped bool
	subsMu  sync.RWMutex
}

func NewBroker(opts ...BrokerOpt) *Broker {
	b := &Broker{
		bufferSize: defaultBufferSize,
		subs:       make(map[chan<- *Event]*subscriber),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *Broker) Broadcast(topic string, data any) {
	event := &Event{
		Topic: topic,
		TS:    time.Now(),
		Data:  data,
	}

	var eg multierror.Group

	b.subsMu.RLock()
	defer b.subsMu.RUnlock()
	if b.stopped {
		return
	}
	for ch, sub := range b.subs {
		eg.Go(func() error {
			return b.send(ch, sub, event)
		})
	}
	err := eg.Wait().ErrorOrNil()
	if err != nil {
		b.logger.Error("error broadcasting event", zap.Error(err))
	}
}

func (b *Broker) send(ch chan<- *Event, sub *subscriber, event *Event) error {
	send := true
	if len(sub.topics) > 0 {
		send = false
		for _, topic := range sub.topics {
			if strings.HasPrefix(topic, event.Topic) {
				send = true
				break
			}
		}
	}
	if !send {
		return nil
	}

	select {
	case ch <- event:
	default:
		b.logger.Warn("subscriber buffer is full, dropping event",
			zap.String("subscriber", sub.name),
			zap.String("topic", event.Topic),
		)
		return nil
	}

	return nil
}

func (b *Broker) Subscribe(ctx context.Context, name string, eventTopics []string) (<-chan *Event, error) {
	sub := &subscriber{
		name:   name,
		topics: eventTopics,
	}
	ch := make(chan *Event, b.bufferSize)

	b.subsMu.Lock()
	b.subs[ch] = sub
	b.subsMu.Unlock()

	// unsubscribe on ctx done
	go func() {
		<-ctx.Done()

		// all channels are closed when the broker is stopped
		b.subsMu.RLock()
		if b.stopped {
			return
		}
		b.subsMu.RUnlock()

		// otherwise, we cancel here
		b.subsMu.Lock()
		delete(b.subs, ch)
		b.subsMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

type subscriber struct {
	name   string
	topics []string
}

func (b *Broker) Stop() {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()

	if b.stopped {
		return
	}

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
	b.stopped = true
}
