package reporter

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/kamaln7/resolvable"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/qnet"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func Test_toEventStoreConnection(t *testing.T) {
	process := &process.Process{
		Exe: "/bin/test",
		Container: resolvable.Static(&process.Container{
			ID:    "container-id",
			Name:  "container-name",
			Image: "container-image",
		}).WithBackgroundContext(),
		Pod: resolvable.Static(&process.Pod{
			Name:      "pod-name",
			Namespace: "pod-namespace",
		}).WithBackgroundContext(),
		UserShell: resolvable.Static(true).WithBackgroundContext(),
	}
	process.SetHostname("local-hostname")
	process.SetUser(1000, "test-user")

	t.Run("ingress connection", func(t *testing.T) {
		conn := &eventstore.Connection{
			Direction: eventstore.Direction_Ingress,
			Part:      0,
			Finalized: true,
			System: &eventstore.ConnectionSystem{
				Hostname:      telemetry.Hostname(),
				Agent:         "tap",
				AgentInstance: telemetry.InstanceID(),
			},
			Tags: map[string][]string{
				"tag1":     {"value1", "value2"},
				"tag2":     {"value"},
				"strategy": {"observe"},
				"host":     {"local-hostname"},
				"ip":       {"5.6.7.8"},
			},
			Labels: []string{
				"user-shell",
			},
			Source: &eventstore.ConnectionEndpointRemote{
				Address: qnet.NetAddr{
					IP:   net.ParseIP("1.2.3.4"),
					Port: 1234,
				},
			},
			Destination: &eventstore.ConnectionEndpointLocal{
				Address: qnet.NetAddr{
					IP:   net.ParseIP("5.6.7.8"),
					Port: 5678,
				},
				Exe:      "/bin/test",
				User:     "test-user",
				UserID:   1000,
				Hostname: "local-hostname",
				Container: &eventstore.Container{
					ID:    "container-id",
					Name:  "container-name",
					Image: "container-image",
					Pod: &eventstore.Pod{
						Name:      "pod-name",
						Namespace: "pod-namespace",
					},
				},
			},
			BytesReceived:  1000,
			BytesSent:      2000,
			TLSVersion:     tlsutils.VersionTLS13,
			SocketProtocol: eventstore.SocketProtocol_TCP,
			L7Protocol:     eventstore.L7Protocol_HTTP1,
		}
		conn.SetEndpointID("example.com")

		origConn := connection.NewConnection(
			t.Context(),
			zaptest.NewLogger(t),
			&connection.OpenEvent{
				Local: qnet.NetAddr{
					IP:   net.ParseIP("5.6.7.8"),
					Port: 5678,
				},
				Remote: qnet.NetAddr{
					IP:   net.ParseIP("1.2.3.4"),
					Port: 1234,
				},
				Source:       connection.Server,
				SocketType:   connection.SocketType_TCP,
				IsRedirected: false,
			},
			connection.WithProcess(process),
			connection.WithTags(tags.FromMultiValues(map[string][]string{
				"tag1": {"value1", "value2"},
				"tag2": {"value"},
			})),
		)
		origConn.SetDomain("example.com")
		origConn.TLSClientHello = &tlsutils.ClientHello{
			Version: tls.VersionTLS13,
		}
		origConn.Protocol = connection.Protocol_HTTP1
		origConn.CloseEvent = &connection.CloseEvent{WrBytes: 2000, RdBytes: 1000}

		// sync up the randomly generated connection ID
		conn.SetConnectionID(origConn.ID())
		assert.Equal(t, conn, toEventStoreConnection(origConn))
	})

	t.Run("egress connection", func(t *testing.T) {
		conn := &eventstore.Connection{
			Direction: eventstore.Direction_EgressExternal,
			Part:      0,
			Finalized: true,
			System: &eventstore.ConnectionSystem{
				Hostname:      telemetry.Hostname(),
				Agent:         "tap",
				AgentInstance: telemetry.InstanceID(),
			},
			Tags: map[string][]string{
				"tag1":     {"value1", "value2"},
				"tag2":     {"value"},
				"strategy": {"observe"},
				"host":     {"local-hostname"},
				"ip":       {"5.6.7.8"},
			},
			Labels: []string{
				"user-shell",
			},
			Source: &eventstore.ConnectionEndpointLocal{
				Address: qnet.NetAddr{
					IP:   net.ParseIP("5.6.7.8"),
					Port: 5678,
				},
				Exe:      "/bin/test",
				User:     "test-user",
				UserID:   1000,
				Hostname: "local-hostname",
				Container: &eventstore.Container{
					ID:    "container-id",
					Name:  "container-name",
					Image: "container-image",
					Pod: &eventstore.Pod{
						Name:      "pod-name",
						Namespace: "pod-namespace",
					},
				},
			},
			Destination: &eventstore.ConnectionEndpointRemote{
				Address: qnet.NetAddr{
					IP:   net.ParseIP("1.2.3.4"),
					Port: 1234,
				},
			},
			BytesReceived:  1000,
			BytesSent:      2000,
			TLSVersion:     tlsutils.VersionTLS13,
			SocketProtocol: eventstore.SocketProtocol_UDP,
			L7Protocol:     eventstore.L7Protocol_HTTP2,
		}
		conn.SetEndpointID("example.com")

		origConn := connection.NewConnection(
			t.Context(),
			zaptest.NewLogger(t),
			&connection.OpenEvent{
				Local: qnet.NetAddr{
					IP:   net.ParseIP("5.6.7.8"),
					Port: 5678,
				},
				Remote: qnet.NetAddr{
					IP:   net.ParseIP("1.2.3.4"),
					Port: 1234,
				},
				Source:       connection.Client,
				SocketType:   connection.SocketType_UDP,
				IsRedirected: false,
			},
			connection.WithProcess(process),
			connection.WithTags(tags.FromMultiValues(map[string][]string{
				"tag1": {"value1", "value2"},
				"tag2": {"value"},
			})),
		)
		origConn.SetDomain("example.com")
		origConn.TLSClientHello = &tlsutils.ClientHello{
			Version: tls.VersionTLS13,
		}
		origConn.CloseEvent = &connection.CloseEvent{WrBytes: 2000, RdBytes: 1000}
		origConn.Protocol = connection.Protocol_HTTP2

		// sync up the randomly generated connection ID
		conn.SetConnectionID(origConn.ID())
		assert.Equal(t, conn, toEventStoreConnection(origConn))
	})
}
