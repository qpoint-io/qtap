package e2e

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

const (
	TypeObjectStore services.ServiceType = "e2e"
)

func NewObjectStore(logger *zap.Logger) *ObjectStoreFactory {
	return &ObjectStoreFactory{
		logger: logger,
	}
}

type ObjectStoreFactory struct {
	objectstore.BaseObjectStore
	logger *zap.Logger
}

func (f *ObjectStoreFactory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *ObjectStoreFactory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	es, err := services.GetService[eventstore.EventStore](ctx, svcRegistry, eventstore.TypeEventStore, "")
	if err != nil {
		return nil, err
	}
	return &ObjectStore{
		eventStore: es,
	}, nil
}

func (f *ObjectStoreFactory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeObjectStore))
}

type ObjectStore struct {
	objectstore.BaseObjectStore
	services.LogHelper
	eventStore eventstore.EventStore
}

func (o *ObjectStore) Put(ctx context.Context, artifact *eventstore.Artifact) {
	o.Log().Debug("forwarding artifact to e2e event store", o.LogFields(artifact)...)
	// forward directly to the e2e event store
	o.eventStore.Save(ctx, artifact)
}
