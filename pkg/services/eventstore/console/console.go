package console

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const (
	TypeConsoleEventStore services.ServiceType = "console"
)

type Factory struct {
	eventstore.BaseEventStore
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *Factory) Create(ctx context.Context) (services.Service, error) {
	return &EventStore{}, nil
}

// ServiceType returns the service type
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
	s.Log().Info("event store submission",
		zap.String("type", fmt.Sprintf("%T", item)),
		zap.Any("item", item),
	)
}
