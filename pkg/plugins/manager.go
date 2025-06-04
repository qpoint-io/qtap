package plugins

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type ConnectionAdapter interface {
	SetConnection(*connection.Connection)
}

const (
	defaultBufferSize = 1024 * 1024 * 10 // 10MB
	PodLabelStack     = "tap.qpoint.io/stack"
)

var tracer = telemetry.Tracer()

type stackKey struct {
	Domain   string
	Protocol string
}

type Manager struct {
	// logger
	logger *zap.Logger

	// buffer size
	bufferSize int

	// stacks
	stacks *synq.Map[string, *Stack]

	// stack config snapshot (JSON)
	configSnapshot string

	// domain -> stack mapping
	domainStacks *synq.Map[stackKey, config.TapHttpConfig]

	// default stack
	defaultStackConfig config.TapHttpConfig

	// service registry
	serviceRegistry *services.ServiceRegistry

	// plugin registry
	pluginRegistry *PluginRegistry

	// mutex
	mu sync.Mutex
}

type ManagerOpt func(*Manager)

func SetBufferSize(bufferSize int) ManagerOpt {
	return func(m *Manager) {
		m.bufferSize = bufferSize
	}
}

func SetServiceRegistry(registry *services.ServiceRegistry) ManagerOpt {
	return func(m *Manager) {
		m.serviceRegistry = registry
	}
}

func SetPluginRegistry(registry *PluginRegistry) ManagerOpt {
	return func(m *Manager) {
		m.pluginRegistry = registry
	}
}

func NewPluginManager(logger *zap.Logger, opts ...ManagerOpt) *Manager {
	manager := &Manager{
		logger:       logger,
		bufferSize:   defaultBufferSize,
		stacks:       synq.NewMap[string, *Stack](),
		domainStacks: synq.NewMap[stackKey, config.TapHttpConfig](),
	}

	// set options
	for _, opt := range opts {
		opt(manager)
	}

	return manager
}

func (m *Manager) Start() error {
	// stacks
	telemetry.ObservableGauge(
		"tap_plugin_stacks",
		func() float64 {
			return float64(m.stacks.Len())
		},
		telemetry.WithDescription("A count of active plugin stacks"),
	)

	// domain stacks
	telemetry.ObservableGauge(
		"tap_plugin_domain_stacks",
		func() float64 {
			return float64(m.domainStacks.Len())
		},
		telemetry.WithDescription("Count of plugin domain to stack mappings"),
	)

	return nil
}

func (m *Manager) SetConfig(conf *config.Config) {
	// create a map of domain -> stack
	m.domainStacks.Reset()

	// loop through the endpoints to find any that have a specific stack
	for _, endpoint := range conf.Tap.Endpoints {
		m.domainStacks.Store(
			stackKey{Domain: endpoint.Domain, Protocol: "http"},
			endpoint.Http,
		)
	}

	// set the default stack
	m.mu.Lock()
	m.defaultStackConfig = conf.Tap.Http
	m.mu.Unlock()

	// generate a snapshot of the incoming config
	snapshot, err := yaml.Marshal(conf.Stacks)
	if err != nil {
		m.logger.Error("marshalling stack config", zap.Error(err))
		return
	}

	// if the snapshot is the same, don't do anything
	if m.configSnapshot == string(snapshot) {
		return
	}

	// persist the snapshot
	m.configSnapshot = string(snapshot)

	// reconcile stacks
	if err := m.reconcileStacks(conf); err != nil {
		m.logger.Error("reconciling stacks", zap.Error(err))
	}
}

// register a domain to a stack
func (m *Manager) registerDomainToStack(domain, protocol string) (*StackDeployment, error) {
	// create a lookup key
	key := stackKey{Domain: domain, Protocol: protocol}

	// lock the manager
	m.mu.Lock()

	// fetch the endpointConfig
	endpointConfig, exists := m.domainStacks.Load(key)
	if !exists {
		endpointConfig = m.defaultStackConfig
	}

	// unlock the manager
	m.mu.Unlock()

	// if we don't have any stacks, return nil
	if !endpointConfig.HasStack() {
		return nil, nil
	}

	// fetch the stack
	stack, exists := m.stacks.Load(endpointConfig.Stack)
	if !exists {
		return nil, fmt.Errorf("stack %s does not exist", endpointConfig)
	}

	// return the active deployment
	return stack.GetActiveDeployment(), nil
}

// GetDomainStack returns the stackID for a domain and protocol
func (m *Manager) GetDomainStack(domain, protocol string) (string, bool) {
	// create a lookup key
	key := stackKey{Domain: domain, Protocol: protocol}

	// check if the domain has a stack
	if stackID, exists := m.domainStacks.Load(key); exists {
		return stackID.Stack, stackID.HasStack()
	}

	// return the default stack
	return m.defaultStackConfig.Stack, m.defaultStackConfig.HasStack()
}

func (m *Manager) Stop() {
	m.stacks.Iter(func(_ string, s *Stack) bool {
		s.Teardown()
		return true
	})
}

func (m *Manager) NewConnection(ctx context.Context, connectionType ConnectionType, conn *connection.Connection) (*Connection, error) {
	ll := conn.Logger()
	if connectionType == ConnectionType_UNKNOWN {
		return nil, errors.New("connection type is required")
	}

	stack, err := m.connectionStack(connectionType, conn)
	if err != nil {
		return nil, fmt.Errorf("resolving connection stack: %w", err)
	}

	if stack == nil {
		ll.Debug("ignoring connection without stack")
		return nil, nil
	}

	svcs := make([]services.Service, 0, len(stack.requiredServices))
	for _, s := range stack.requiredServices {
		svc := m.serviceRegistry.Get(s)
		if svc == nil {
			return nil, fmt.Errorf("service %s not found", s)
		}

		svcInstance, err := svc.Create(ctx)
		if err != nil {
			return nil, fmt.Errorf("creating service: %w", err)
		}

		// check for adapters
		if l, ok := svcInstance.(services.LoggerAdapter); ok {
			l.SetLogger(conn.Logger())
		}
		if c, ok := svcInstance.(ConnectionAdapter); ok {
			c.SetConnection(conn)
		}

		svcs = append(svcs, svcInstance)
	}

	return NewConnection(ctx, conn.Logger(), conn.ID(), m.bufferSize, connectionType, stack, conn.Tags(), svcs), nil
}

func (m *Manager) reconcileStacks(conf *config.Config) error {
	// reconcile stacks
	for name, stack := range conf.Stacks {
		// if stack exists (keyed by name), update
		if s, exists := m.stacks.Load(name); exists {
			if err := s.SetConfig(&stack); err != nil {
				return fmt.Errorf("setting stack config: %w", err)
			}
		} else {
			// create a stack
			s := NewStack(name, m.logger, m.pluginRegistry)

			// setup
			if err := s.SetConfig(&stack); err != nil {
				return fmt.Errorf("setting up stack: %w", err)
			}

			// add to the map
			m.stacks.Store(name, s)
		}
	}

	// now find the stacks that have been removed from the config
	m.stacks.Iter(func(name string, _ *Stack) bool {
		if _, exists := conf.Stacks[name]; !exists {
			m.stacks.Delete(name)
		}
		return true
	})

	return nil
}

// connectionStack resolves the stack deployment for a connection
func (m *Manager) connectionStack(typ ConnectionType, conn *connection.Connection) (*StackDeployment, error) {
	ll := conn.Logger()
	// use pod label if set
	if pod := conn.Process().Pod; pod != nil {
		if stackName, ok := pod.Labels[PodLabelStack]; ok {
			ll = ll.With(
				zap.String("stack", stackName),
				zap.String("pod", pod.Name),
				zap.String("pod_namespace", pod.Namespace),
			)
			stack, ok := m.stacks.Load(stackName)
			if ok {
				ll.Debug("resolved connection stack from pod label")
				return stack.GetActiveDeployment(), nil
			} else {
				ll.Warn("ignoring invalid pod stack label")
			}
		}
	}

	// resolve stack by domain, falling back to default stack
	return m.registerDomainToStack(conn.Domain(), string(typ))
}
