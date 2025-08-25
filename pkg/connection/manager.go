package connection

import (
	"context"
	"reflect"
	"strings"
	"time"

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
	connections *synq.Map[Cookie, *ManagedConnection]

	// sweeper management
	sweeperCancel context.CancelFunc

	// sweeper configuration
	sweeperInterval     time.Duration
	idleTimeout         time.Duration
	closedTimeout       time.Duration
	finalizationTimeout time.Duration
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
		logger:              logger,
		connections:         synq.NewMap[Cookie, *ManagedConnection](),
		sweeperInterval:     DefaultSweeperInterval,
		idleTimeout:         DefaultIdleTimeout,
		closedTimeout:       DefaultClosedTimeout,
		finalizationTimeout: DefaultFinalizationTimeout,
	}
	for _, opt := range opts {
		opt(m)
	}

	telemetry.RegisterCollector(newManagerMetrics(m))

	// Start the connection sweeper
	// TODO:add start/stop funcs
	m.startSweeper()

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

	if managedConn, exists := m.connections.Load(event.Key()); exists {
		// In most cases, processOpenEvent() will set the process. Very rarely,
		// a race will occur where the connection is created without the process.
		// More details on this in processOpenEvent().
		//
		// As a backup, attempt to rediscover the process on following events.
		if managedConn.Process() == nil && managedConn.OpenEvent != nil {
			proc := m.processManager.Get(int(managedConn.OpenEvent.PID))
			if proc != nil {
				m.logger.Info("discovered process", zap.Int("pid", proc.Pid))
				managedConn.SetProcess(proc)
			}
		}

		// Update event time and push to queue
		managedConn.UpdateEventTime()

		// Handle state transitions for certain events
		if closeEvent, ok := event.(CloseEvent); ok {
			managedConn.processCloseEvent(closeEvent)
		}

		if err := managedConn.eventQueue.Push(event); err != nil {
			m.logger.Error("failed to push event to connection queue", zap.Error(err))
		}
		return
	} else {
		m.logger.Warn("⚠️ connection not found", zap.Any("cookie", event.Key()), zap.String("event_name", strings.TrimPrefix(reflect.TypeOf(event).String(), "connection.")))
	}
}

func (m *Manager) finalizeConnection(conn *Connection) {
	conn.logger.Debug("deleting connection from manager map")
	m.connections.Delete(conn.cookie)
}

func (m *Manager) createStreamer(conn *Connection) StreamProcessor {
	return m.streamFactory.OnConnection(conn)
}

// startSweeper begins the connection lifecycle management goroutine
func (m *Manager) startSweeper() {
	ctx, cancel := context.WithCancel(context.Background())
	m.sweeperCancel = cancel

	go func() {
		ticker := time.NewTicker(m.sweeperInterval)
		defer ticker.Stop()

		m.logger.Debug("connection sweeper started",
			zap.Duration("interval", m.sweeperInterval))

		for {
			select {
			case <-ctx.Done():
				m.logger.Debug("connection sweeper stopped")
				return
			case <-ticker.C:
				m.sweepConnections()
			}
		}
	}()
}

// stopSweeper stops the connection lifecycle management goroutine
// This is useful for clean shutdown and testing
func (m *Manager) stopSweeper() {
	if m.sweeperCancel != nil {
		m.sweeperCancel()
		m.sweeperCancel = nil
	}
}

// sweepConnections processes all connections for lifecycle management
func (m *Manager) sweepConnections() {
	totalConnections := m.connections.Len()
	if totalConnections == 0 {
		return
	}

	var (
		processed        int
		stateTransitions int
		cleaned          int
	)

	m.connections.Iter(func(cookie Cookie, managedConn *ManagedConnection) bool {
		processed++

		switch managedConn.State() {
		case StateOpen:
			if managedConn.IsIdle(m.idleTimeout) {
				m.logger.Debug("connection idle timeout, initiating close",
					zap.String("conn_id", managedConn.ID()),
					zap.Duration("idle_time", managedConn.TimeSinceLastEvent()))

				// Trigger graceful closure by transitioning to closing
				if managedConn.TransitionTo(StateClosing) {
					stateTransitions++
					// We don't call Close() here - let the normal close event flow handle it
					// or transition to finalizing if already closed
					if managedConn.CloseEvent != nil && !managedConn.held {
						managedConn.Close()
					}
				}
			}

		case StateClosing:
			if managedConn.HasBeenInStateFor(m.closedTimeout) {
				if managedConn.CanFinalize() || managedConn.HasBeenInStateFor(2*m.closedTimeout) {
					m.logger.Debug("transitioning connection to finalizing",
						zap.String("conn_id", managedConn.ID()),
						zap.Duration("time_in_closing", managedConn.TimeSinceStateChange()),
						zap.Bool("can_finalize", managedConn.CanFinalize()))

					if managedConn.TransitionTo(StateFinalizing) {
						stateTransitions++
						// Start the finalization process
						go func(conn *ManagedConnection) {
							conn.Close()
						}(managedConn)
					}
				}
			}

		case StateFinalizing:
			if managedConn.HasBeenInStateFor(m.finalizationTimeout) {
				m.logger.Debug("finalizing connection cleanup timeout",
					zap.String("conn_id", managedConn.ID()),
					zap.Duration("time_in_finalizing", managedConn.TimeSinceStateChange()))

				if managedConn.TransitionTo(StateFinalized) {
					stateTransitions++
				}
			}

		case StateFinalized:
			m.logger.Debug("removing finalized connection from manager",
				zap.String("conn_id", managedConn.ID()))
			m.connections.Delete(cookie)
			cleaned++
		}

		return true // Continue iteration
	})

	if processed > 0 {
		m.logger.Debug("connection sweep completed",
			zap.Int("total_connections", totalConnections),
			zap.Int("processed", processed),
			zap.Int("state_transitions", stateTransitions),
			zap.Int("cleaned", cleaned))
	}
}
