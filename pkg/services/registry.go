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

// ServiceRegistry is a convenience type for a registry of services
type ServiceRegistry struct {
	*Registry[ServiceType, Service]
}

func NewServiceRegistry(services ...Service) *ServiceRegistry {
	sr := NewRegistrySize[ServiceType, Service](len(services))
	for _, service := range services {
		sr.Register(service.ServiceType(), service)
	}
	return &ServiceRegistry{sr}
}

func (sr *ServiceRegistry) Register(value Service) {
	sr.Registry.Register(value.ServiceType(), value)
}

func (sr *ServiceRegistry) Copy() *ServiceRegistry {
	return &ServiceRegistry{sr.Registry.Copy()}
}

// FactoryRegistry is a convenience type for a registry of service factories.
// Factory registries are keyed by ServiceType() and not FactoryType() so this wrapper exists to avoid any confusion by users.
type FactoryRegistry struct {
	*Registry[ServiceType, ServiceFactory]
}

func NewFactoryRegistry(factories ...ServiceFactory) *FactoryRegistry {
	fr := NewRegistrySize[ServiceType, ServiceFactory](len(factories))
	for _, factory := range factories {
		fr.Register(factory.ServiceType(), factory)
	}
	return &FactoryRegistry{fr}
}

func (fr *FactoryRegistry) Register(value ServiceFactory) {
	fr.Registry.Register(value.ServiceType(), value)
}

func (fr *FactoryRegistry) Copy() *FactoryRegistry {
	return &FactoryRegistry{fr.Registry.Copy()}
}

func (fr *FactoryRegistry) CreateService(ctx context.Context, serviceType ServiceType) (Service, error) {
	factory := fr.Get(serviceType)
	if factory == nil {
		return nil, fmt.Errorf("factory not found for service %s", serviceType)
	}
	return factory.Create(ctx)
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
