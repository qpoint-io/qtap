package reporter

import (
	"context"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

type service struct {
	conn                *connection.Connection
	eventStore          eventstore.EventStore
	logToConsole        bool
	firstReportDeadline time.Duration
	reportInterval      time.Duration
	logger              *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc

	reportCount uint32
	mu          sync.Mutex
}

// SetConnection implements the ConnectionAdapter interface.
func (s *service) SetConnection(conn *connection.Connection) {
	s.conn = conn

	// we have the connection, we can start reporting now
	go s.start()
}

// SetLogger implements the LoggerAdapter interface.
func (s *service) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

func (s *service) start() {
	// kick off the first report
	if s.firstReportDeadline > 0 {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(s.firstReportDeadline):
			// send the first report
			s.report()
		}
	}

	// start the recurring reports
	s.startRecurringReports()
}

func (s *service) startRecurringReports() {
	if s.reportInterval == 0 || s.ctx.Err() != nil {
		return
	}

	tick := time.Tick(s.reportInterval)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick:
			s.report()
		}
	}
}

func (s *service) report() {
	ok := s.mu.TryLock()
	if !ok {
		// if the mutex is locked, another report is already in progress
		return
	}
	defer s.mu.Unlock()

	// if the domain is empty it means the destination IP had a 0 length and something was
	// interupted in the socket layer. The connection is invalid and we can safely ignore it.
	if s.conn.Domain() == "" {
		return
	}

	connection := toEventStoreConnection(s.conn)
	connection.Part = s.reportCount // set event store specific part number

	// generate the log
	s.eventStore.Save(s.conn.Context(), connection)

	// debug log
	if s.logToConsole {
		s.logger.Debug("connection report", zap.Any("connection", connection))
	}

	// increment report count for next report
	s.reportCount++
}

func (s *service) Close() error {
	// cancel the tickers
	s.cancel()

	// send a final report
	s.report()
	return nil
}

func (s *service) ServiceType() services.ServiceType {
	return Type
}

func toEventStoreConnection(conn *connection.Connection) *eventstore.Connection {
	c := &eventstore.Connection{
		Finalized: conn.CloseEvent != nil,
		Timestamp: conn.CreatedAt(), // [DEPRECATED]
		CreatedAt: conn.CreatedAt(),
		ClosedAt:  conn.ClosedAt(),
		System: &eventstore.ConnectionSystem{
			Hostname:      telemetry.Hostname(),
			Agent:         "tap",
			AgentInstance: telemetry.InstanceID(),
		},
		L7Protocol: toEventStoreL7Protocol(conn.Protocol),
		Labels:     conn.Labels().Items(),
	}
	c.SetConnectionID(conn.ID())
	c.SetEndpointID(conn.Domain())

	if t := conn.Tags(); t != nil {
		c.Tags = t.Map()
	}

	if conn.CloseEvent != nil {
		c.BytesReceived = uint64(conn.CloseEvent.RdBytes)
		c.BytesSent = uint64(conn.CloseEvent.WrBytes)
	}

	// Prefer ServerHello (negotiated values) over ClientHello (proposed values)
	if conn.TLSServerHello != nil {
		c.TLSVersion = conn.TLSServerHello.Version
	} else if conn.TLSClientHello != nil {
		c.TLSVersion = conn.TLSClientHello.Version
	}

	// For TLS connections that have data events we can resolve
	// that the connection was introspected by one of the probes
	if conn.IsTLS && conn.DataEventCount() > 0 {
		c.TLSIntrospected = true
	}

	if conn.OpenEvent != nil {
		c.SocketProtocol = toEventStoreSocketType(conn.OpenEvent.SocketType)
		localEndpoint := &eventstore.ConnectionEndpointLocal{
			Address: conn.OpenEvent.Local,
			PID:     conn.OpenEvent.PID,
		}
		if proc := conn.Process(); proc != nil {
			localEndpoint.Exe = proc.Exe

			if hostname, _ := proc.Hostname(); hostname != "" {
				localEndpoint.Hostname = hostname
			}
			if user, _ := proc.User(); user != nil {
				localEndpoint.User = user.Username
				localEndpoint.UserID = user.UID
			}
			if container, _ := proc.Container(); container != nil {
				pod, _ := proc.Pod() // pod can be nil
				localEndpoint.Container = toEventStoreContainer(container, pod)
			}
			if tlsProbes := proc.TLSProbeTypesDetected; tlsProbes == nil || len(tlsProbes) > 0 {
				c.TLSProbeTypesDetected = tlsProbes
			}
		}

		remoteEndpoint := &eventstore.ConnectionEndpointRemote{
			Address: conn.OpenEvent.Remote,
		}

		// client vs server
		switch conn.OpenEvent.Source {
		case connection.Client:
			if conn.OpenEvent.Remote.IP.IsPrivate() {
				c.Direction = eventstore.Direction_EgressInternal
			} else {
				c.Direction = eventstore.Direction_EgressExternal
			}
			c.Source = localEndpoint
			c.Destination = remoteEndpoint

		case connection.Server:
			c.Direction = eventstore.Direction_Ingress
			c.Source = remoteEndpoint
			c.Destination = localEndpoint
		}
	}

	return c
}

func toEventStoreSocketType(socketType connection.SocketType) eventstore.SocketProtocol {
	switch socketType {
	case connection.SocketType_TCP:
		return eventstore.SocketProtocol_TCP
	case connection.SocketType_UDP:
		return eventstore.SocketProtocol_UDP
	case connection.SocketType_RAW:
		return eventstore.SocketProtocol_RAW
	case connection.SocketType_ICMP:
		return eventstore.SocketProtocol_ICMP
	default:
		return ""
	}
}

func toEventStoreL7Protocol(protocol connection.Protocol) eventstore.L7Protocol {
	switch protocol {
	case connection.Protocol_HTTP1:
		return eventstore.L7Protocol_HTTP1
	case connection.Protocol_HTTP2:
		return eventstore.L7Protocol_HTTP2
	case connection.Protocol_DNS:
		return eventstore.L7Protocol_DNS
	case connection.Protocol_GRPC:
		return eventstore.L7Protocol_GRPC
	case connection.Protocol_MYSQL:
		return eventstore.L7Protocol_MYSQL
	case connection.Protocol_REDIS:
		return eventstore.L7Protocol_REDIS
	case connection.Protocol_POSTGRES:
		return eventstore.L7Protocol_POSTGRES
	default:
		return eventstore.L7Protocol_OTHER
	}
}

func toEventStoreContainer(container *process.Container, pod *process.Pod) *eventstore.Container {
	if container == nil {
		return nil
	}
	c := &eventstore.Container{
		ID:    container.ID,
		Name:  container.Name,
		Image: container.Image,
	}

	if pod != nil {
		c.Pod = &eventstore.Pod{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		}
	}

	return c
}
