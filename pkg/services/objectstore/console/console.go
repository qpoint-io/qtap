package console

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

// ensure we implement the ObjectStore interface
var _ objectstore.ObjectStore = (*ObjectStore)(nil)

const (
	TypeConsoleObjectStore services.ServiceType = "console"
)

type Factory struct {
	objectstore.BaseObjectStore
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &ObjectStore{}, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeConsoleObjectStore))
}

type ObjectStore struct {
	services.LogHelper
	objectstore.BaseObjectStore
}

func (s *ObjectStore) Put(ctx context.Context, artifact *eventstore.Artifact) {
	fields := append(s.LogFields(artifact),
		zap.String("data_base64", base64.StdEncoding.EncodeToString(artifact.Data)))

	s.Log().Debug("object store submission", fields...)
}
