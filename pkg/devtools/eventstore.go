package devtools

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const (
	TypeEventStore services.ServiceType = "devtools"
)

type EventStoreFactory struct {
	eventstore.BaseEventStore

	logger    *zap.Logger
	broadcast func(*Event)
}

func (f *EventStoreFactory) Init(ctx context.Context, cfg any) error {
	return nil
}

// Create implements the services.ServiceFactory interface
func (f *EventStoreFactory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &EventStore{save: f.save}, nil
}

func (f *EventStoreFactory) save(item any) {
	var topic string
	switch i := item.(type) {
	case *eventstore.GrpcRequest:
		topic = "request.grpc"
	case *eventstore.Request:
		topic = "request.created"
	case *eventstore.Issue:
		topic = "issue.created"
	case *eventstore.PIIEntity:
		topic = "pii.created"
	case *eventstore.Connection:
		topic = "connection.opened"
		if i.Finalized {
			topic = "connection.closed"
		} else if i.Part > 0 {
			topic = "connection.updated"
		}
	case *eventstore.DatabaseRequest:
		topic = "request.database"
	default:
		f.logger.Error("unknown event type", zap.String("type", fmt.Sprintf("%T", item)))
		return
	}

	f.broadcast(NewEvent(topic, map[string]any{
		"data": item,
	}))
}

// FactoryType returns the service type
func (f *EventStoreFactory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", f.ServiceType(), TypeEventStore))
}

type EventStore struct {
	eventstore.BaseEventStore
	save func(item any)
}

func (s *EventStore) Save(ctx context.Context, item any) {
	s.save(item)
}
