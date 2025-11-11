package services

import (
	"context"
	"fmt"
)

// ServiceType represents a type identifier for services
type ServiceType string

func (s ServiceType) String() string {
	return string(s)
}

// Service is the base interface that all services must implement
// Services are connection-scoped and are created by a Factory.
type Service interface {
	// ServiceType returns the type of service.
	//
	// For example, an eventstore that prints to the console would have a ServiceType of "eventstore".
	ServiceType() ServiceType
}

// FactoryFn creates a service factory
type FactoryFn func() Factory

// Factory creates service instances
type Factory interface {
	// Init initializes the service factory
	Init(ctx context.Context, config any) error
	// Create creates a new service instance
	// svcRegistry is used to resolve any cross-service dependencies.
	Create(ctx context.Context, svcRegistry *ServiceRegistry) (Service, error)
	// FactoryType returns the type of factory. This is typically a namespaced instance of the ServiceType.
	//
	// For example, an eventstore that prints to the console would have a FactoryType of "eventstore.console" and a ServiceType of "eventstore".
	FactoryType() ServiceType
	// ServiceType returns the type of service this factory creates
	ServiceType() ServiceType
}

// NextFactory indicates that a factory will handle graceful replacements
// of itself to the replacement factory
type NextFactory interface {
	Next(Factory)
}

// StaticFactory is a factory that always returns the same service instance.
func StaticFactory(factoryType ServiceType, service Service) Factory {
	return &staticFactory{
		factoryType: factoryType,
		service:     service,
	}
}

type staticFactory struct {
	factoryType ServiceType
	service     Service
}

func (f *staticFactory) FactoryType() ServiceType {
	return f.factoryType
}

func (f *staticFactory) ServiceType() ServiceType {
	return f.service.ServiceType()
}

func (f *staticFactory) Create(ctx context.Context, svcRegistry *ServiceRegistry) (Service, error) {
	return f.service, nil
}

func (f *staticFactory) Init(ctx context.Context, config any) error {
	return nil
}

// GetService retrieves a service by type and optional ID from a registry and casts it into the requested type.
func GetService[T any](ctx context.Context, sr *ServiceRegistry, key ServiceKey) (T, error) {
	var zero T
	service, err := sr.Get(ctx, key)
	if err != nil {
		return zero, fmt.Errorf("getting service %q: %w", key, err)
	}
	t, ok := service.(T)
	if !ok {
		return zero, fmt.Errorf("service %q is not of type %T", key, zero)
	}
	return t, nil
}

// ServiceKey is a key for a service in a registry.
type ServiceKey struct {
	Type ServiceType
	ID   string
}

func (k ServiceKey) String() string {
	if k.ID != "" {
		return fmt.Sprintf("%s:%s", k.Type, k.ID)
	}
	return k.Type.String()
}
