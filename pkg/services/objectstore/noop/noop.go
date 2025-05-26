package noop

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
)

const (
	TypeNoopObjectStore services.ServiceType = "noop"
)

type Factory struct {
	objectstore.BaseObjectStore
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *Factory) Create(ctx context.Context) (services.Service, error) {
	return &ObjectStore{}, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeNoopObjectStore))
}

type ObjectStore struct {
	objectstore.BaseObjectStore
}

func (s *ObjectStore) Put(artifact eventstore.Artifact) (*eventstore.ArtifactRecord, error) {
	return nil, nil
}
