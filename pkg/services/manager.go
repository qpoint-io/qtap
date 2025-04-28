package services

import (
	"context"
	"io"

	"github.com/qpoint-io/qtap/pkg/config"
	"go.uber.org/zap"
)

// ServiceManager handles service creation and lifecycle
type ServiceManager struct {
	ctx       context.Context
	logger    *zap.Logger
	registry  *ServiceRegistry
	factories map[ServiceType]ServiceFactory
}

// NewServiceManager creates a new service manager
func NewServiceManager(ctx context.Context, logger *zap.Logger, registry *ServiceRegistry) *ServiceManager {
	return &ServiceManager{
		ctx:       ctx,
		logger:    logger,
		registry:  registry,
		factories: make(map[ServiceType]ServiceFactory),
	}
}

// RegisterFactory registers a service factory
func (sm *ServiceManager) RegisterFactory(factory ...ServiceFactory) {
	for _, f := range factory {
		if _, exists := sm.factories[f.FactoryType()]; !exists {
			sm.logger.Debug("registering factory", zap.String("service_type", f.FactoryType().String()))
			sm.factories[f.FactoryType()] = f
		}
	}
}

// SetConfig processes a config update and creates/updates services
func (sm *ServiceManager) SetConfig(config *config.Config) {
	if config == nil {
		return
	}

	for key, svcConfig := range config.Services.ToMap() {
		factory, exists := sm.factories[ServiceType(key)]
		if !exists {
			sm.logger.Debug("no factory registered for service type", zap.String("service_type", key))
			continue
		}

		// Close old service if it exists and implements Closer
		if old := sm.registry.Get(ServiceType(key)); old != nil {
			if closer, ok := old.(io.Closer); ok {
				defer func() {
					closer.Close()
				}()
			}
		}

		// Set the registry for the factory if it implements the SetRegistry interface
		if sr, ok := factory.(SetRegistry); ok {
			sr.SetRegistry(sm.registry)
		}

		sm.logger.Info("initializing service factory", zap.String("factory_type", key))
		if err := factory.Init(sm.ctx, svcConfig); err != nil {
			sm.logger.Error("failed to initialize service factory", zap.String("factory_type", key), zap.Error(err))
			continue
		}

		sm.registry.Register(factory)
	}
}
