package connection

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/process"
	servicespkg "github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

type Keyer interface {
	Key() Cookie
}

type ConnectionStreamer interface {
	OnConnection(conn *Connection) StreamProcessor
}

type StreamProcessor interface {
	Process(event *DataEvent) error
	Close()
	Closed() bool
}

type Manager struct {
	// internal components
	logger          *zap.Logger
	processManager  *process.Manager
	dnsManager      *dns.DNSManager
	streamFactory   ConnectionStreamer
	controlManager  ControlManager
	serviceRegistry *servicespkg.ServiceRegistry

	// deployment tags
	deploymentTags tags.List

	// config
	config *config.Config

	// connections
	connections *synq.Map[Cookie, *Connection]
}

type ManagerOpt func(*Manager)

func SetProcessManager(pm *process.Manager) ManagerOpt {
	return func(m *Manager) {
		m.processManager = pm
	}
}

func SetDnsManager(dm *dns.DNSManager) ManagerOpt {
	return func(m *Manager) {
		m.dnsManager = dm
	}
}

func SetStreamFactory(sf ConnectionStreamer) ManagerOpt {
	return func(m *Manager) {
		m.streamFactory = sf
	}
}

func SetServiceRegistry(sr *servicespkg.ServiceRegistry) ManagerOpt {
	return func(m *Manager) {
		m.serviceRegistry = sr
	}
}

func SetConfig(conf *config.Config) ManagerOpt {
	return func(m *Manager) {
		m.config = conf
	}
}

func SetDeploymentTags(tags tags.List) ManagerOpt {
	return func(m *Manager) {
		m.deploymentTags = tags
	}
}

func SetControlManager(cm ControlManager) ManagerOpt {
	return func(m *Manager) {
		m.controlManager = cm
	}
}

func NewManager(logger *zap.Logger, opts ...ManagerOpt) *Manager {
	m := &Manager{
		logger:      logger,
		connections: synq.NewMap[Cookie, *Connection](),
	}
	for _, opt := range opts {
		opt(m)
	}

	telemetry.RegisterCollector(newManagerMetrics(m))

	return m
}

func (m *Manager) SetConfig(conf *config.Config) {
	m.config = conf
}

func (m *Manager) HandleEvent(event Keyer) {
	// debug
	// m.logger.Debug("handling event",
	// 	zap.Stringer("id", id),
	// 	zap.String("type", reflect.TypeOf(event).String()),
	// 	zap.String("event", fmt.Sprintf("%+v", event)))

	// special handling for some events because we setup
	// the connection and pairing events are handled
	if e, ok := event.(OpenEvent); ok {
		m.processOpenEvent(e)
		return
	}

	if conn, exists := m.connections.Load(event.Key()); exists {
		if err := conn.eventQueue.Push(event); err != nil {
			m.logger.Error("failed to push event to connection queue", zap.Error(err))
		}
		return
	}
}

func (m *Manager) finalizeConnection(conn *Connection) {
	conn.logger.Debug("deleting connection from manager map")
	m.connections.Delete(conn.cookie)
}

func (m *Manager) createStreamer(conn *Connection) StreamProcessor {
	return m.streamFactory.OnConnection(conn)
}
