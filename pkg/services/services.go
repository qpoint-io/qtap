package services

import (
	"context"
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
	Create(ctx context.Context) (Service, error)
	// FactoryType returns the type of factory. This is typically a namespaced instance of the ServiceType.
	//
	// For example, an eventstore that prints to the console would have a FactoryType of "eventstore.console" and a ServiceType of "eventstore".
	FactoryType() ServiceType
	// ServiceType returns the type of service this factory creates
	ServiceType() ServiceType
}

// SetFactoryRegistry sets the factory registry for the service
type SetFactoryRegistry interface {
	SetFactoryRegistry(registry *FactoryRegistry)
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

func (f *staticFactory) Create(ctx context.Context) (Service, error) {
	return f.service, nil
}

func (f *staticFactory) Init(ctx context.Context, config any) error {
	return nil
}
