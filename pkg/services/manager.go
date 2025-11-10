package services

import (
	"context"
	"io"

	"github.com/qpoint-io/qtap/pkg/config"
	"go.uber.org/zap"
)

// ServiceManager handles service creation and lifecycle
type ServiceManager struct {
	ctx           context.Context
	logger        *zap.Logger
	registry      *FactoryRegistry
	factories     *Registry[ServiceType, FactoryFactory]
	extraServices []func(*config.Config) *config.ServiceConfig
}

// NewServiceManager creates a new service manager
func NewServiceManager(ctx context.Context, logger *zap.Logger, registry *FactoryRegistry) *ServiceManager {
	return &ServiceManager{
		ctx:       ctx,
		logger:    logger,
		registry:  registry,
		factories: NewRegistry[ServiceType, FactoryFactory](),
	}
}

func (sm *ServiceManager) AddExtraServices(services ...func(*config.Config) *config.ServiceConfig) {
	sm.extraServices = append(sm.extraServices, services...)
}

// RegisterFactory registers a service factory
func (sm *ServiceManager) RegisterFactory(fns ...FactoryFactory) {
	for _, fn := range fns {
		factory := fn()
		if _, exists := sm.factories.Load(factory.FactoryType()); !exists {
			sm.logger.Debug("registering factory", zap.String("factory_type", factory.FactoryType().String()))
			sm.factories.Register(factory.FactoryType(), fn)
		}
	}
}

// SetConfig processes a config update and creates/updates services
func (sm *ServiceManager) SetConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}

	// get services from config
	svcs := cfg.Services.AllConfigs()
	for _, fn := range sm.extraServices {
		svcs = append(svcs, fn(cfg))
	}

	for _, svcConfig := range svcs {
		fn := sm.factories.Get(ServiceType(svcConfig.Type))
		if fn == nil {
			sm.logger.Debug("no factory registered for service type", zap.String("service_type", svcConfig.Type))
			continue
		}

		factory := fn()

		// Close old service if it exists and implements Closer
		if old := sm.registry.Get(factory.ServiceType()); old != nil {
			// closes factories that are closeable
			if closer, ok := old.(io.Closer); ok {
				defer func() {
					closer.Close()
				}()
			}

			// sends replacement factory to services that support it
			if next, ok := old.(NextFactory); ok {
				defer func() {
					next.Next(factory)
				}()
			}
		}

		// Set the registry for the factory if it implements the SetRegistry interface
		if sr, ok := factory.(SetFactoryRegistry); ok {
			sr.SetFactoryRegistry(sm.registry)
		}

		sm.logger.Info(
			"initializing service factory",
			zap.String("factory_type", svcConfig.Type),
			zap.Stringer("service_type", factory.ServiceType()),
		)
		if err := factory.Init(sm.ctx, svcConfig.Config); err != nil {
			sm.logger.Error("failed to initialize service factory", zap.String("factory_type", svcConfig.Type), zap.Error(err))
			continue
		}

		if svcConfig.ID != "" {
			sm.registry.Register(factory, svcConfig.ID)
		}
		// register it as the default if explicitly specified or no ID was provided
		if svcConfig.Default || svcConfig.ID == "" {
			sm.registry.Register(factory, "")
		}
	}
}
