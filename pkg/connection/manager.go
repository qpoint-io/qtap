package connection

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/log"
	"github.com/qpoint-io/qtap/pkg/process"
	servicespkg "github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
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
	logger             *zap.Logger
	processManager     *process.Manager
	dnsManager         *dns.DNSManager
	streamFactory      ConnectionStreamer
	controlManager     ControlManager
	svcFactoryRegistry *servicespkg.FactoryRegistry

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

func SetServiceFactoryRegistry(fr *servicespkg.FactoryRegistry) ManagerOpt {
	return func(m *Manager) {
		m.svcFactoryRegistry = fr
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
		// In most cases, processOpenEvent() will set the process. Very rarely,
		// a race will occur where the connection is created without the process.
		// More details on this in processOpenEvent().
		//
		// As a backup, attempt to rediscover the process on following events.
		if conn.Process() == nil && conn.OpenEvent != nil {
			proc := m.processManager.Get(int(conn.OpenEvent.PID))
			if proc != nil {
				m.logger.Debug("discovered process", zap.Int("pid", proc.Pid))
				conn.SetProcess(proc)
			}
		}

		if err := conn.eventQueue.Push(event); err != nil {
			m.logger.Error("failed to push event to connection queue", zap.Error(err))
		}
		return
	}
}

func (m *Manager) finalizeConnection(conn *Connection) {
	conn.logger.Log(log.TraceLevel, "deleting connection from manager map")
	m.connections.Delete(conn.cookie)
}

func (m *Manager) createStreamer(conn *Connection) StreamProcessor {
	return m.streamFactory.OnConnection(conn)
}
