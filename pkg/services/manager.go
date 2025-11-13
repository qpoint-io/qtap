package services

import (
	"context"
	"io"

	"github.com/qpoint-io/qtap/pkg/config"
	"go.uber.org/zap"
)

// FactoryManager handles factory creation and configuration
type FactoryManager struct {
	ctx           context.Context
	logger        *zap.Logger
	factories     *FactoryRegistry
	factoryFns    *Registry[ServiceType, FactoryFn]
	extraServices []func(*config.Config) *config.ServiceConfig
}

// NewFactoryManager creates a new factory manager
func NewFactoryManager(ctx context.Context, logger *zap.Logger, registry *FactoryRegistry) *FactoryManager {
	return &FactoryManager{
		ctx:        ctx,
		logger:     logger,
		factories:  registry,
		factoryFns: NewRegistry[ServiceType, FactoryFn](),
	}
}

// AddExtraServices adds services that are always added to the config.
func (m *FactoryManager) AddExtraServices(services ...func(*config.Config) *config.ServiceConfig) {
	m.extraServices = append(m.extraServices, services...)
}

// RegisterFactory registers a service factory
func (m *FactoryManager) RegisterFactory(fns ...FactoryFn) {
	for _, fn := range fns {
		factory := fn()
		if _, exists := m.factoryFns.Load(factory.FactoryType()); !exists {
			m.logger.Debug("registering factory", zap.String("factory_type", factory.FactoryType().String()))
			m.factoryFns.Register(factory.FactoryType(), fn)
		}
	}
}

// SetConfig processes a config update and creates/updates services
func (m *FactoryManager) SetConfig(cfg *config.Config) {
	if cfg == nil {
		// TODO: should we clear all existing factories in this case?
		return
	}

	// get services from config
	svcs := cfg.Services.All()
	for _, fn := range m.extraServices {
		svcs = append(svcs, fn(cfg))
	}

	for _, svcConfig := range svcs {
		fn := m.factoryFns.Get(ServiceType(svcConfig.Type))
		if fn == nil {
			m.logger.Debug("no factory registered for service", zap.String("service_type", svcConfig.Type))
			continue
		}

		factory := fn()

		logger := m.logger.With(
			zap.Stringer("service_type", factory.ServiceType()),
			zap.String("factory_type", svcConfig.Type),
		)
		if svcConfig.ID != "" {
			logger = logger.With(zap.String("service_id", svcConfig.ID))
		}

		logger.Info("initializing service factory")
		if err := factory.Init(m.ctx, svcConfig.Config); err != nil {
			logger.Error("failed to initialize service factory", zap.Error(err))
			continue
		}

		if svcConfig.ID != "" {
			m.registerFactory(factory, svcConfig.ID)
		}
		// register it as the default if explicitly specified or no ID was provided
		if svcConfig.Default || svcConfig.ID == "" {
			m.registerFactory(factory, "")
		}
	}
}

func (m *FactoryManager) registerFactory(factory Factory, id string) {
	key := ServiceKey{Type: factory.ServiceType(), ID: id}
	old := m.factories.Get(key)
	if old != nil {
		// gracefully replace the old factory if it exists
		defer m.replaceFactory(id, old, factory)
	}
	m.factories.Register(factory, id)
}

func (m *FactoryManager) replaceFactory(id string, old Factory, new Factory) {
	logger := m.logger.With(
		zap.Stringer("old_factory_type", old.FactoryType()),
		zap.Stringer("new_factory_type", new.FactoryType()),
	)
	if id != "" {
		logger = logger.With(zap.String("service_id", id))
	}
	logger.Debug("replacing factory")
	// send replacement factory to services that support it
	if next, ok := old.(NextFactory); ok {
		next.Next(new)
	}

	// close old factory if it implements Closer
	if closer, ok := old.(io.Closer); ok {
		closer.Close()
	}
}
