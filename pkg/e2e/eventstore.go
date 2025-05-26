package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const (
	TypeEventStore services.ServiceType = "e2e"
)

type Events struct {
	Requests    []*eventstore.Request
	Issues      []*eventstore.Issue
	Artifacts   []*eventstore.Artifact
	PIIEntities []*eventstore.PIIEntity
	Connections []*eventstore.Connection
}

type EventStore struct {
	eventstore.BaseEventStore

	mu           sync.Mutex
	logger       *zap.Logger
	events       *Events
	byConnection map[string]*Events
	byCtxID      map[string][]string
	errors       []error
}

func NewEventStore(logger *zap.Logger) *EventStore {
	return &EventStore{
		logger:       logger,
		byConnection: make(map[string]*Events),
		byCtxID:      make(map[string][]string),
		events:       &Events{},
	}
}

func (f *EventStore) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *EventStore) Create(ctx context.Context) (services.Service, error) {
	return f, nil
}

// ServiceType returns the service type
func (f *EventStore) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, TypeEventStore))
}

func (e *Events) Add(item any) (string, bool) {
	switch i := item.(type) {
	case *eventstore.Request:
		e.Requests = append(e.Requests, i)
		return i.ConnectionID, true
	case *eventstore.Issue:
		e.Issues = append(e.Issues, i)
		return i.ConnectionID, true
	case *eventstore.Artifact:
		e.Artifacts = append(e.Artifacts, i)
		return i.ConnectionID, true
	case *eventstore.PIIEntity:
		e.PIIEntities = append(e.PIIEntities, i)
		return i.ConnectionID, true
	case *eventstore.Connection:
		e.Connections = append(e.Connections, i)
		return i.ConnectionID, true
	default:
		return "", false
	}
}

func (e *Events) Merge(other *Events) {
	e.Requests = append(e.Requests, other.Requests...)
	e.Issues = append(e.Issues, other.Issues...)
	e.Artifacts = append(e.Artifacts, other.Artifacts...)
	e.PIIEntities = append(e.PIIEntities, other.PIIEntities...)
	e.Connections = append(e.Connections, other.Connections...)
}

// Save stores an event
func (f *EventStore) Save(ctx context.Context, item any) {
	f.logger.Debug("saving event", zap.Any("item", item), zap.String("type", fmt.Sprintf("%T", item)))
	f.mu.Lock()
	defer f.mu.Unlock()

	connid, ok := f.events.Add(item)
	if !ok {
		f.errors = append(f.errors, fmt.Errorf("unknown event type: %T", item))
		return
	}

	if f.byConnection[connid] == nil {
		f.byConnection[connid] = &Events{}
	}
	f.byConnection[connid].Add(item)

	if conn, ok := item.(*eventstore.Connection); ok {
		if conn.Tags != nil {
			for _, id := range conn.Tags["ctxid"] {
				f.byCtxID[id] = append(f.byCtxID[id], connid)
			}
		}
	}
}

func (f *EventStore) Close() error {
	return nil
}

func (f *EventStore) Errors() ([]error, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.errors, len(f.errors) == 0
}

func (f *EventStore) GetByCtxID(id string) *Events {
	f.mu.Lock()
	defer f.mu.Unlock()

	var events Events
	for _, connid := range f.byCtxID[id] {
		if f.byConnection[connid] != nil {
			events.Merge(f.byConnection[connid])
		}
	}
	return &events
}

// AwaitByCtxID waits for at least numConnections to be saved for a given ctxid
func (e *EventStore) AwaitByCtxID(id string, numConnections int, timeout time.Duration) (*Events, error) {
	if numConnections == 0 {
		return e.GetByCtxID(id), nil
	}

	deadline := time.Now().Add(timeout)
	for {
		if timeout > 0 && time.Now().After(deadline) {
			return nil, fmt.Errorf("exceeded %s timeout while waiting for %d connections for ctxid %s", timeout, numConnections, id)
		}

		e.logger.Info(fmt.Sprintf("waiting for %d connections", numConnections), zap.String("ctxid", id))
		events := e.GetByCtxID(id)
		if len(events.Connections) >= numConnections {
			return events, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}
