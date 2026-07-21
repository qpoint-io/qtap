package connection

import (
	"context"
	"sync"

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
	svcFactoryRegistry *servicespkg.FactoryRegistry

	// deployment tags
	deploymentTags tags.List

	// config
	config *config.Config

	// connections
	connections *synq.Map[Cookie, *Connection]

	admissionMu sync.RWMutex
	stopping    bool

	activeMu         sync.Mutex
	active           map[*Connection]bool
	shutdownDone     chan struct{}
	shutdownWaiting  bool
	shutdownComplete bool
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

func NewManager(logger *zap.Logger, opts ...ManagerOpt) *Manager {
	m := &Manager{
		logger:       logger,
		connections:  synq.NewMap[Cookie, *Connection](),
		active:       make(map[*Connection]bool),
		shutdownDone: make(chan struct{}),
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
	m.admissionMu.RLock()
	defer m.admissionMu.RUnlock()

	if m.stopping {
		return
	}

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
	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	m.activeMu.Lock()
	if _, ok := m.active[conn]; ok {
		m.active[conn] = true
	}
	m.activeMu.Unlock()

	conn.logger.Log(log.TraceLevel, "deleting connection from manager map")
	if current, ok := m.connections.Load(conn.cookie); ok && current == conn {
		m.connections.Delete(conn.cookie)
	}
}

func (m *Manager) trackConnection(conn *Connection) {
	m.activeMu.Lock()
	defer m.activeMu.Unlock()
	m.active[conn] = false
}

func (m *Manager) completeConnection(conn *Connection) {
	m.activeMu.Lock()
	defer m.activeMu.Unlock()

	delete(m.active, conn)
	if m.shutdownWaiting && len(m.active) == 0 && !m.shutdownComplete {
		m.shutdownComplete = true
		close(m.shutdownDone)
	}
}

// Shutdown stops event admission and waits for all active connections to drain
// their queues and close their connection-scoped services.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.admissionMu.Lock()
	start := !m.stopping
	m.stopping = true
	m.admissionMu.Unlock()

	if start {
		m.activeMu.Lock()
		m.shutdownWaiting = true
		for conn, finalizing := range m.active {
			if !finalizing {
				m.active[conn] = true
				conn.requestShutdown()
			}
		}
		if len(m.active) == 0 {
			m.shutdownComplete = true
			close(m.shutdownDone)
		}
		m.activeMu.Unlock()
	}

	select {
	case <-m.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) createStreamer(conn *Connection) StreamProcessor {
	return m.streamFactory.OnConnection(conn)
}
