package noop

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
)

const (
	TypeNoopEventStore services.ServiceType = "noop"
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

// ServiceType returns the service type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, TypeNoopEventStore))
}

// EventStore implements the EventStore interface with Postgres
type EventStore struct {
	eventstore.BaseEventStore
}

// Save stores an event
func (s *EventStore) Save(ctx context.Context, item any) {}
