package e2e

import (
	"context"
	"fmt"
	"sync"

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
	byCGID       map[string][]string
	errors       []error
}

func NewEventStore(logger *zap.Logger) *EventStore {
	return &EventStore{
		logger:       logger,
		byConnection: make(map[string]*Events),
		byCGID:       make(map[string][]string),
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
			for _, id := range conn.Tags["cgid"] {
				f.byCGID[id] = append(f.byCGID[id], connid)
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

func (f *EventStore) GetByCGID(cgid string) Events {
	f.mu.Lock()
	defer f.mu.Unlock()

	var events Events
	for _, connid := range f.byCGID[cgid] {
		if f.byConnection[connid] != nil {
			events.Merge(f.byConnection[connid])
		}
	}
	return events
}
