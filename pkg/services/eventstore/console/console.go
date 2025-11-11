package console

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
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

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	os, err := services.GetService[objectstore.ObjectStore](ctx, svcRegistry, services.ServiceKey{Type: objectstore.TypeObjectStore})
	if err != nil {
		return nil, fmt.Errorf("getting object store: %w", err)
	}

	return &EventStore{
		objectStore: os,
	}, nil
}

// ServiceType returns the service type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, TypeConsoleEventStore))
}

// EventStore implements the EventStore interface with Postgres
type EventStore struct {
	services.LogHelper
	eventstore.BaseEventStore

	objectStore objectstore.ObjectStore
}

// Save stores an event
func (s *EventStore) Save(ctx context.Context, item any) {
	if l, ok := s.objectStore.(services.LoggerAdapter); ok {
		l.SetLogger(s.Log())
	}

	switch i := item.(type) {
	case *eventstore.Artifact:
		go func() {
			ar, err := s.objectStore.Put(*i)
			if err != nil {
				s.Log().Error("failed to put artifact", zap.Error(err))
				return
			}
			s.Save(ctx, ar)
		}()
	default:
		s.Log().Info("event store submission",
			zap.String("type", fmt.Sprintf("%T", item)),
			zap.Any("item", item))
	}
}
