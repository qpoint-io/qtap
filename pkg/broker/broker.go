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

type AnyEvent interface {
	Topic() string
	Any() any
}

type Event[T any] struct {
	ts   time.Time
	data *T
}

type BrokerOpt func(*Broker)

func SetBufferSize(bufferSize int) BrokerOpt {
	return func(b *Broker) {
		b.bufferSize = bufferSize
	}
}

type Broker struct {
	bufferSize int

	logger *zap.Logger
	subs   map[chan<- AnyEvent]*subscriber
	subsMu sync.RWMutex
}

func NewBroker(opts ...BrokerOpt) *Broker {
	b := &Broker{
		bufferSize: defaultBufferSize,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *Broker) Broadcast(event AnyEvent) error {
	b.subsMu.RLock()
	defer b.subsMu.RUnlock()

	var eg multierror.Group
	for ch, sub := range b.subs {
		eg.Go(func() error {
			return b.send(ch, sub, event)
		})
	}
	return eg.Wait().ErrorOrNil()
}

func (b *Broker) send(ch chan<- AnyEvent, sub *subscriber, event AnyEvent) error {
	send := true
	if len(sub.topics) > 0 {
		send = false
		for _, topic := range sub.topics {
			if strings.HasPrefix(topic, event.Topic()) {
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
			zap.String("topic", event.Topic()),
		)
		return nil
	}

	return nil
}

func (b *Broker) Subscribe(ctx context.Context, name string, eventTopics []string) (<-chan AnyEvent, error) {
	sub := &subscriber{
		name:   name,
		topics: eventTopics,
	}
	ch := make(chan AnyEvent, b.bufferSize)

	b.subsMu.Lock()
	b.subs[ch] = sub
	b.subsMu.Unlock()

	// unsubscribe on ctx done
	go func() {
		<-ctx.Done()

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
