package services

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/qpoint-io/qtap/pkg/synq"
)

/*
	Convenience types
*/

// FactoryRegistry is a convenience type for a registry of service factories.
// Factory registries are keyed by ServiceType() and not FactoryType() so this wrapper exists to avoid any confusion by users.
type FactoryRegistry struct {
	r *Registry[ServiceType, ServiceFactory]
}

func NewFactoryRegistry(factories ...ServiceFactory) *FactoryRegistry {
	fr := NewRegistrySize[ServiceType, ServiceFactory](len(factories))
	for _, factory := range factories {
		fr.Register(factory.ServiceType(), factory)
	}
	return &FactoryRegistry{fr}
}

// Register registers a default service factory by type and optional ID.
// If the ID is empty, the factory is registered as the default for the type.
func (fr *FactoryRegistry) Register(value ServiceFactory, id string) {
	key := value.ServiceType()
	if id != "" {
		key = ServiceType(fmt.Sprintf("%s:%s", key.String(), id))
	}
	fr.r.Register(key, value)
}

// Load retrieves a service factory by type and optional ID.
// If the ID is empty, the default factory for the type is returned.
func (fr *FactoryRegistry) Load(serviceType ServiceType, id string) (ServiceFactory, bool) {
	key := serviceType
	if id != "" {
		key = ServiceType(fmt.Sprintf("%s:%s", serviceType.String(), id))
	}
	return fr.r.Load(key)
}

// Get retrieves a service factory by type and optional ID.
// If the ID is empty, the default factory for the type is returned.
func (fr *FactoryRegistry) Get(serviceType ServiceType, id string) ServiceFactory {
	f, ok := fr.Load(serviceType, id)
	if !ok {
		return nil
	}
	return f
}

func (fr *FactoryRegistry) Copy() *FactoryRegistry {
	return &FactoryRegistry{fr.r.Copy()}
}

// CreateService creates a new service instance from the default factory for the given type.
func (fr *FactoryRegistry) CreateService(ctx context.Context, serviceType ServiceType, id string) (Service, error) {
	factory, ok := fr.Load(serviceType, id)
	if !ok {
		key := serviceType.String()
		if id != "" {
			key = fmt.Sprintf("%s:%s", key, id)
		}
		return nil, fmt.Errorf("factory not found for service %s", key)
	}
	return factory.Create(ctx)
}

// ServiceRegistry is a convenience type for a registry of services
type ServiceRegistry struct {
	r *Registry[ServiceType, Service]
}

func NewServiceRegistry(services ...Service) *ServiceRegistry {
	sr := NewRegistrySize[ServiceType, Service](len(services))
	for _, service := range services {
		sr.Register(service.ServiceType(), service)
	}
	return &ServiceRegistry{sr}
}

func (sr *ServiceRegistry) Register(value Service) {
	sr.r.Register(value.ServiceType(), value)
}

func (sr *ServiceRegistry) Get(serviceType ServiceType) Service {
	return sr.r.Get(serviceType)
}

func (sr *ServiceRegistry) Copy() *ServiceRegistry {
	return &ServiceRegistry{sr.r.Copy()}
}

func (sr *ServiceRegistry) Close() error {
	return sr.r.Close()
}

/*
	Registry pattern implementation
*/

type Registry[K comparable, T any] struct {
	m *synq.Map[K, T]
}

func NewRegistry[K comparable, T any]() *Registry[K, T] {
	return NewRegistrySize[K, T](0)
}

func NewRegistrySize[K comparable, T any](size int) *Registry[K, T] {
	return &Registry[K, T]{
		m: synq.NewMapSize[K, T](size),
	}
}

func (sr *Registry[K, T]) Register(key K, value T) {
	sr.m.Store(key, value)
}

func (sr *Registry[K, T]) Load(key K) (T, bool) {
	return sr.m.Load(key)
}

func (sr *Registry[K, T]) Get(key K) T {
	value, ok := sr.m.Load(key)
	if !ok {
		var zero T
		return zero
	}

	return value
}

func (sr *Registry[K, T]) Close() error {
	var errs []error
	sr.m.Iter(func(key K, value T) bool {
		if closer, ok := any(value).(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		return true
	})
	return errors.Join(errs...)
}

func (sr *Registry[K, T]) Len() int {
	return sr.m.Len()
}

func (sr *Registry[K, T]) Copy() *Registry[K, T] {
	return &Registry[K, T]{
		m: sr.m.Clone(),
	}
}
