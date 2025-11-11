package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

/*
	Convenience types
*/

// FactoryRegistry is a convenience type for a registry of service factories.
// Factory registries are keyed by ServiceType() and not FactoryType() so this wrapper exists to avoid any confusion by users.
type FactoryRegistry struct {
	r *Registry[ServiceType, Factory]
}

func NewFactoryRegistry(factories ...Factory) *FactoryRegistry {
	fr := NewRegistrySize[ServiceType, Factory](len(factories))
	for _, factory := range factories {
		fr.Register(factory.ServiceType(), factory)
	}
	return &FactoryRegistry{fr}
}

// Register registers a default service factory by type and optional ID.
// If the ID is empty, the factory is registered as the default for the type.
func (fr *FactoryRegistry) Register(value Factory, id string) {
	key := keyWithID(value.ServiceType(), id)
	fr.r.Register(key, value)
}

// Load retrieves a service factory by type and optional ID.
// If the ID is empty, the default factory for the type is returned.
func (fr *FactoryRegistry) Load(serviceType ServiceType, id string) (Factory, bool) {
	key := keyWithID(serviceType, id)
	return fr.r.Load(key)
}

// Get retrieves a service factory by type and optional ID.
// If the ID is empty, the default factory for the type is returned.
func (fr *FactoryRegistry) Get(serviceType ServiceType, id string) Factory {
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
		key := keyWithID(serviceType, id)
		return nil, fmt.Errorf("factory not found for service %s", key)
	}
	return factory.Create(ctx)
}

// ServiceRegistry is a connection-scoped registry that handles creating services from factories.
// This is used to apply adapters and maintain a singleton store for service instances.
type ServiceRegistry struct {
	services     *Registry[ServiceType, Service]
	factories    *FactoryRegistry
	configurator ServiceConfigurator

	mu sync.Mutex
}

type ServiceConfigurator func(ctx context.Context, service Service) (Service, error)

func NewServiceRegistry(fr *FactoryRegistry) *ServiceRegistry {
	return &ServiceRegistry{
		services:  NewRegistry[ServiceType, Service](),
		factories: fr,
	}
}

// SetConfigurator sets a configurator function for any service configuration that needs to be done once it is created.
// This is typically used to apply adapters.
func (sr *ServiceRegistry) SetConfigurator(configurator ServiceConfigurator) {
	sr.configurator = configurator
}

// Get retrieves a service by type and optional ID.
// If the such a service hasn't been created yet, it will be created using the corresponding factory, configured, and returned on future Get calls.
func (sr *ServiceRegistry) Get(ctx context.Context, serviceType ServiceType, id string) (Service, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	key := keyWithID(serviceType, id)
	service, ok := sr.services.Load(key)
	if ok {
		// the service has already been created
		return service, nil
	}

	// create the service and store it
	zap.L().Debug("creating service", zap.Stringer("service", key))
	service, err := sr.factories.CreateService(ctx, serviceType, id)
	if err != nil {
		return nil, fmt.Errorf("creating service: %w", err)
	}

	// configure the service
	if sr.configurator != nil {
		service, err = sr.configurator(ctx, service)
		if err != nil {
			return nil, fmt.Errorf("configuring service: %w", err)
		}
	}

	sr.services.Register(key, service)
	return service, nil
}

func (sr *ServiceRegistry) Close() error {
	return sr.services.Close()
}

// Has checks if the registry is able to return the given service type and ID.
func (sr *ServiceRegistry) Has(serviceType ServiceType, id string) bool {
	_, ok := sr.factories.Load(serviceType, id)
	return ok
}

func GetService[T any](ctx context.Context, sr *ServiceRegistry, serviceType ServiceType, id string) (T, error) {
	var zero T
	service, err := sr.Get(ctx, serviceType, id)
	if err != nil {
		return zero, fmt.Errorf("getting service %q: %w", keyWithID(serviceType, id), err)
	}
	t, ok := service.(T)
	if !ok {
		return zero, fmt.Errorf("service %q is not of type %T", keyWithID(serviceType, id), zero)
	}
	return t, nil
}

func keyWithID[T ~string](serviceType T, id string) T {
	if id != "" {
		return T(fmt.Sprintf("%s:%s", serviceType, id))
	}
	return serviceType
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
