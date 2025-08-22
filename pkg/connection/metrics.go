package connection

import (
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
)

func newManagerMetrics(m *Manager) *managerMetrics {
	return &managerMetrics{m: m}
}

type managerMetrics struct {
	m *Manager

	activeConnections    telemetry.GaugeFn
	activeConnectionsTLS telemetry.GaugeFn
}

func (m *managerMetrics) Register(factory telemetry.Factory) {
	m.activeConnections = factory.Gauge(
		"tap_active_connections",
		telemetry.WithDescription("The number of active connections"),
		telemetry.WithLabels(
			// connection.SocketType
			"socket_type",
		),
	)

	m.activeConnectionsTLS = factory.Gauge(
		"tap_active_connections_tls",
		telemetry.WithDescription("The number of active TLS connections"),
		telemetry.WithLabels(
			// tlsutils.TLSVersion
			"version",
		),
	)
}

func (m *managerMetrics) Collect() {
	var (
		// activeConnections
		connectionsByType = map[SocketType]int{}
		// activeConnectionsTLS
		connectionsByTLSVersion = map[tlsutils.TLSVersion]int{}
	)

	m.m.connections.Iter(func(key Cookie, managedConn *ManagedConnection) bool {
		if managedConn.OpenEvent != nil {
			connectionsByType[managedConn.OpenEvent.SocketType]++
		}
		if managedConn.TLSClientHello != nil {
			connectionsByTLSVersion[managedConn.TLSClientHello.Version]++
		}
		return true
	})

	for socketType, count := range connectionsByType {
		m.activeConnections(float64(count), string(socketType))
	}

	for tlsVersion, count := range connectionsByTLSVersion {
		m.activeConnectionsTLS(float64(count), tlsVersion.String())
	}
}
