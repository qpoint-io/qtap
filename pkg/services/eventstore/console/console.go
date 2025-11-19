package console

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

// ensure we implement the EventStore interface
var _ eventstore.EventStore = (*EventStore)(nil)

const (
	TypeConsoleEventStore services.ServiceType = "console"
)

type Factory struct {
	eventstore.BaseEventStore
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &EventStore{}, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, TypeConsoleEventStore))
}

// EventStore implements the EventStore interface with Postgres
type EventStore struct {
	services.LogHelper
	eventstore.BaseEventStore
}

// Save stores an event
func (s *EventStore) Save(ctx context.Context, item any) {
	ll := s.Log()
	switch i := item.(type) {
	case *eventstore.Artifact:
		ll.DPanic("event stores do not support artifacts", zap.Any("artifact", i))
		return
	default:
		ll.Info("event store submission",
			zap.String("type", fmt.Sprintf("%T", item)),
			zap.Any("item", item))
	}
}
