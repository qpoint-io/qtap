package objectstore

import (
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
)

const (
	TypeObjectStore services.ServiceType = "objectstore"
)

// ObjectStore defines the interface for object storage services
type ObjectStore interface {
	services.Service
	Put(artifact eventstore.Artifact) (*eventstore.ArtifactRecord, error)
}

// BaseObjectStore provides common functionality for ObjectStore implementations
type BaseObjectStore struct{}

// ServiceType returns the service type
func (b *BaseObjectStore) ServiceType() services.ServiceType {
	return TypeObjectStore
}
