package devtools

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

const (
	TypeObjectStore services.ServiceType = "devtools"
)

type ObjectStoreFactory struct {
	objectstore.BaseObjectStore

	logger    *zap.Logger
	broadcast func(*Event)
}

func (f *ObjectStoreFactory) Init(ctx context.Context, cfg any) error {
	return nil
}

// Create implements the services.ServiceFactory interface
func (f *ObjectStoreFactory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &ObjectStore{put: f.put}, nil
}

func (f *ObjectStoreFactory) put(artifact *eventstore.Artifact) {
	topic := "artifact.created"
	if artifact.Type == eventstore.ArtifactType_HTTPTransaction {
		topic = "request.http_transaction"
	}

	f.broadcast(NewEvent(topic, map[string]any{
		"data": artifact,
	}))
}

// FactoryType returns the service type
func (f *ObjectStoreFactory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", f.ServiceType(), TypeObjectStore))
}

type ObjectStore struct {
	objectstore.BaseObjectStore
	put func(artifact *eventstore.Artifact)
}

func (s *ObjectStore) Put(ctx context.Context, artifact *eventstore.Artifact) {
	s.put(artifact)
}
