package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/rulekit/set"
	"go.uber.org/zap"
)

const (
	TypeEventStore services.ServiceType = "e2e"
)

type EventStoreFactory struct {
	eventstore.BaseEventStore

	mu              sync.Mutex
	logger          *zap.Logger
	events          *Events
	eventsByConn    map[string]*Events
	connIDsByCtxIDs map[string]set.Set[string]
	errors          []error
}

func NewEventStore(logger *zap.Logger) *EventStoreFactory {
	return &EventStoreFactory{
		logger:          logger,
		eventsByConn:    make(map[string]*Events),
		connIDsByCtxIDs: make(map[string]set.Set[string]),
		events:          &Events{},
	}
}

func (f *EventStoreFactory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *EventStoreFactory) Create(ctx context.Context) (services.Service, error) {
	return &EventStore{
		save: f.save,
	}, nil
}

func (f *EventStoreFactory) save(conn *connection.Connection, item any) {
	f.logger.Debug("saving event",
		zap.String("type", fmt.Sprintf("%T", item)),
		zap.Any("item", item))
	f.mu.Lock()
	defer f.mu.Unlock()

	if conn != nil {
		if c, ok := item.(connidable); ok {
			c.SetConnectionID(conn.ID())
		}

		if t, ok := item.(taggable); ok {
			t.AddTags(conn.Tags().List()...)
		}

		if f.eventsByConn[conn.ID()] == nil {
			f.eventsByConn[conn.ID()] = &Events{}
		}
		_ = f.eventsByConn[conn.ID()].Add(item)

		if conn.Tags() != nil {
			tags := conn.Tags().Map()
			for _, id := range tags["ctxid"] {
				if f.connIDsByCtxIDs[id] == nil {
					f.connIDsByCtxIDs[id] = make(set.Set[string])
				}
				f.connIDsByCtxIDs[id].Add(conn.ID())
			}
		}
	} else {
		f.logger.Warn("got event to a store instance without a connection")
	}

	err := f.events.Add(item)
	if err != nil {
		f.errors = append(f.errors, err)
	}
}

// ServiceType returns the service type
func (f *EventStoreFactory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, TypeEventStore))
}

func (f *EventStoreFactory) Errors() ([]error, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.errors, len(f.errors) == 0
}

func (f *EventStoreFactory) GetByCtxID(id string) *Events {
	f.mu.Lock()
	defer f.mu.Unlock()

	var events Events
	for _, connid := range f.connIDsByCtxIDs[id].Items() {
		if f.eventsByConn[connid] != nil {
			events.Merge(f.eventsByConn[connid])
		}
	}
	return &events
}

// AwaitByCtxID waits for at least numConnections to be saved for a given ctxid
func (f *EventStoreFactory) AwaitByCtxID(id string, numConnections int, timeout time.Duration) (*Events, error) {
	if numConnections == 0 {
		return f.GetByCtxID(id), nil
	}

	ll := f.logger.With(zap.String("ctxid", id))
	deadline := time.Now().Add(timeout)
	first := true
	for {
		if timeout > 0 && time.Now().After(deadline) {
			return nil, fmt.Errorf("exceeded %s timeout while waiting for %d connections for ctxid %s", timeout, numConnections, id)
		}

		events := f.GetByCtxID(id)
		if first {
			ll.Info(fmt.Sprintf("waiting for %d connections", numConnections))
		} else {
			ll.Debug(fmt.Sprintf("waiting for %d connections (got %d)", numConnections, len(events.Connections)))
		}

		if len(events.Connections) >= numConnections {
			ll.Info(fmt.Sprintf("found %d ≥ %d connections", len(events.Connections), numConnections))
			return events, nil
		}
		time.Sleep(100 * time.Millisecond)
		first = false
	}
}

type connidable interface {
	SetConnectionID(id string)
}

type taggable interface {
	AddTags(tag ...string)
}

type EventStore struct {
	eventstore.BaseEventStore
	conn *connection.Connection
	save func(conn *connection.Connection, item any)
}

func (s *EventStore) SetConnection(conn *connection.Connection) {
	s.conn = conn
}

// Save stores an event
func (s *EventStore) Save(ctx context.Context, item any) {
	s.save(s.conn, item)
}

type Events struct {
	Requests    []*eventstore.Request
	Issues      []*eventstore.Issue
	Artifacts   []*eventstore.Artifact
	PIIEntities []*eventstore.PIIEntity
	Connections []*eventstore.Connection
}

func (e *Events) Add(item any) error {
	switch i := item.(type) {
	case *eventstore.Request:
		e.Requests = append(e.Requests, i)
	case *eventstore.Issue:
		e.Issues = append(e.Issues, i)
	case *eventstore.Artifact:
		e.Artifacts = append(e.Artifacts, i)
	case *eventstore.PIIEntity:
		e.PIIEntities = append(e.PIIEntities, i)
	case *eventstore.Connection:
		e.Connections = append(e.Connections, i)
	default:
		return fmt.Errorf("unknown event type: %T", item)
	}
	return nil
}

func (e *Events) Merge(other *Events) {
	e.Requests = append(e.Requests, other.Requests...)
	e.Issues = append(e.Issues, other.Issues...)
	e.Artifacts = append(e.Artifacts, other.Artifacts...)
	e.PIIEntities = append(e.PIIEntities, other.PIIEntities...)
	e.Connections = append(e.Connections, other.Connections...)
}
