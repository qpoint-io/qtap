package objectstore

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const (
	TypeObjectStore services.ServiceType = "objectstore"
)

// ObjectStore defines the interface for object storage services
//
//go:generate go tool go.uber.org/mock/mockgen -destination ./objectstore_mock.go -package objectstore . ObjectStore
type ObjectStore interface {
	services.Service
	Put(ctx context.Context, artifact *eventstore.Artifact)
}

// BaseObjectStore provides common functionality for ObjectStore implementations
type BaseObjectStore struct{}

// ServiceType returns the service type
func (o *BaseObjectStore) ServiceType() services.ServiceType {
	return TypeObjectStore
}

func (o *BaseObjectStore) LogFields(artifact *eventstore.Artifact) []zap.Field {
	return []zap.Field{
		zap.String("type", artifact.Type.String()),
		zap.String("contentType", artifact.ContentType),
		zap.Int("bytes", len(artifact.Data)),
		zap.String("digest", artifact.Digest()),
	}
}
