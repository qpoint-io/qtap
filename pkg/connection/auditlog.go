package connection

import (
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

func (m *Manager) generateAuditLog(conn *Connection) {
	if conn.eventStore == nil {
		conn.logger.Debug("generateAuditLog: no event store, skipping")
		return
	}

	if conn.HandlerType == HandlerType_FORWARDING {
		conn.logger.Debug("generateAuditLog: forwarding connection detected, ignoring audit")
		return
	}

	// if this is DNS, ensure we're wanted
	if m.config != nil && conn.Protocol == Protocol_DNS && !m.config.Tap.AuditIncludeDNS {
		m.logger.Debug("generateAuditLog: DNS audit log disabled",
			zap.String("conn_pid_id", conn.connPIDKey.String()),
			zap.String("local_addr", conn.OpenEvent.Local.String()),
			zap.String("remote_addr", conn.OpenEvent.Remote.String()))
		return
	}

	// if the domain is '<nil>' it means the destination IP had a 0 length and something was
	// interupted in the socket layer. The connection is invalid and we can safely ignore it.
	if strings.EqualFold(conn.Domain(), "<nil>") {
		m.logger.Debug("generateAuditLog: domain is <nil>, ignoring",
			zap.String("conn_pid_id", conn.connPIDKey.String()),
			zap.String("local_addr", conn.OpenEvent.Local.String()),
			zap.String("remote_addr", conn.OpenEvent.Remote.String()))
		return
	}

	// audit logs are disabled, nothing to do
	if m.config != nil {
		if !m.config.Services.HasAnyEventStores() {
			m.logger.Debug("generateAuditLog: audit logs disabled",
				zap.String("conn_pid_id", conn.connPIDKey.String()),
				zap.String("local_addr", conn.OpenEvent.Local.String()),
				zap.String("remote_addr", conn.OpenEvent.Remote.String()))
			return
		}
	}

	// set original destination if available
	if od := conn.OriginalDestination; od != nil {
		conn.OpenEvent.Remote = *od
	}

	connection := toEventStoreConnection(conn)
	connection.Timestamp = time.Now()

	// generate the log
	conn.eventStore.Save(conn.ctx, connection)

	// debug log
	conn.logger.Debug("audit log", zap.Any("connection", connection))

	// increment report count for next report
	conn.auditCount++
}

func toEventStoreConnection(conn *Connection) *eventstore.Connection {
	c := &eventstore.Connection{
		Finalized: conn.CloseEvent != nil,
		Part:      conn.auditCount,
		System: &eventstore.ConnectionSystem{
			Hostname:      telemetry.Hostname(),
			Agent:         "tap",
			AgentInstance: telemetry.InstanceID(),
		},
		L7Protocol: toEventStoreL7Protocol(conn.Protocol),
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

	if conn.TLSClientHello != nil {
		c.TLSVersion = conn.TLSClientHello.Version
	}

	if conn.OpenEvent != nil {
		c.SocketProtocol = toEventStoreSocketType(conn.OpenEvent.SocketType)
		localEndpoint := &eventstore.ConnectionEndpointLocal{
			Address: conn.OpenEvent.Local,
		}
		if proc := conn.process; proc != nil {
			localEndpoint.Exe = proc.Exe

			if hostname, _ := proc.Hostname(); hostname != "" {
				localEndpoint.Hostname = hostname
			}
			if user, _ := proc.User(); user != "" {
				localEndpoint.User = user
			}
			if proc.Container != nil {
				localEndpoint.Container = toEventStoreContainer(proc.Container, proc.Pod)
			}
		}

		remoteEndpoint := &eventstore.ConnectionEndpointRemote{
			Address: conn.OpenEvent.Remote,
		}

		// client vs server
		switch conn.OpenEvent.Source {
		case Client:
			if conn.OpenEvent.Remote.IP.IsPrivate() {
				c.Direction = eventstore.Direction_EgressInternal
			} else {
				c.Direction = eventstore.Direction_EgressExternal
			}
			c.Source = localEndpoint
			c.Destination = remoteEndpoint

		case Server:
			c.Direction = eventstore.Direction_Ingress
			c.Source = remoteEndpoint
			c.Destination = localEndpoint
		}
	}

	return c
}

func toEventStoreSocketType(socketType SocketType) eventstore.SocketProtocol {
	switch socketType {
	case SocketType_TCP:
		return eventstore.SocketProtocol_TCP
	case SocketType_UDP:
		return eventstore.SocketProtocol_UDP
	case SocketType_RAW:
		return eventstore.SocketProtocol_RAW
	case SocketType_ICMP:
		return eventstore.SocketProtocol_ICMP
	default:
		return ""
	}
}

func toEventStoreL7Protocol(protocol Protocol) eventstore.L7Protocol {
	switch protocol {
	case Protocol_HTTP1:
		return eventstore.L7Protocol_HTTP1
	case Protocol_HTTP2:
		return eventstore.L7Protocol_HTTP2
	case Protocol_DNS:
		return eventstore.L7Protocol_DNS
	case Protocol_GRPC:
		return eventstore.L7Protocol_GRPC
	default:
		return eventstore.L7Protocol_OTHER
	}
}

func toEventStoreContainer(container *process.Container, pod *process.Pod) *eventstore.Container {
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
