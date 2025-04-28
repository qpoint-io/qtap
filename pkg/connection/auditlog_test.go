package connection

import (
	"crypto/tls"
	"net"
	"testing"

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
		Container: &process.Container{
			ID:    "container-id",
			Name:  "container-name",
			Image: "container-image",
		},
		Pod: &process.Pod{
			Name:      "pod-name",
			Namespace: "pod-namespace",
		},
	}
	process.SetHostname("local-hostname")
	process.SetUser(1000, "test-user")

	t.Run("ingress connection", func(t *testing.T) {
		conn := &eventstore.Connection{
			Direction: eventstore.Direction_Ingress,
			Part:      1,
			Finalized: true,
			System: &eventstore.ConnectionSystem{
				Hostname:      telemetry.Hostname(),
				Agent:         "tap",
				AgentInstance: telemetry.InstanceID(),
			},
			Tags: map[string][]string{
				"tag1": {"value1", "value2"},
				"tag2": {"value"},
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
		conn.SetConnectionID("test-conn-id")
		conn.SetEndpointID("example.com")

		assert.Equal(t,
			conn,
			toEventStoreConnection(&Connection{
				logger:     zaptest.NewLogger(t),
				id:         "test-conn-id",
				domain:     "example.com",
				auditCount: 1,
				tags: tags.FromMultiValues(map[string][]string{
					"tag1": {"value1", "value2"},
					"tag2": {"value"},
				}),
				TLSClientHello: &tlsutils.ClientHello{
					Version: tls.VersionTLS13,
				},
				Protocol: Protocol_HTTP1,
				OpenEvent: &OpenEvent{
					Local: qnet.NetAddr{
						IP:   net.ParseIP("5.6.7.8"),
						Port: 5678,
					},
					Remote: qnet.NetAddr{
						IP:   net.ParseIP("1.2.3.4"),
						Port: 1234,
					},
					Source:       Server,
					SocketType:   SocketType_TCP,
					IsRedirected: false,
				},
				CloseEvent: &CloseEvent{
					WrBytes: 2000,
					RdBytes: 1000,
				},
				process: process,
			}),
		)
	})

	t.Run("egress connection", func(t *testing.T) {
		conn := &eventstore.Connection{
			Direction: eventstore.Direction_EgressExternal,
			Part:      1,
			Finalized: true,
			System: &eventstore.ConnectionSystem{
				Hostname:      telemetry.Hostname(),
				Agent:         "tap",
				AgentInstance: telemetry.InstanceID(),
			},
			Tags: map[string][]string{
				"tag1": {"value1", "value2"},
				"tag2": {"value"},
			},
			Source: &eventstore.ConnectionEndpointLocal{
				Address: qnet.NetAddr{
					IP:   net.ParseIP("5.6.7.8"),
					Port: 5678,
				},
				Exe:      "/bin/test",
				User:     "test-user",
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
		conn.SetConnectionID("test-conn-id")
		conn.SetEndpointID("example.com")

		assert.Equal(t,
			conn,
			toEventStoreConnection(&Connection{
				logger:     zaptest.NewLogger(t),
				id:         "test-conn-id",
				domain:     "example.com",
				auditCount: 1,
				tags: tags.FromMultiValues(map[string][]string{
					"tag1": {"value1", "value2"},
					"tag2": {"value"},
				}),
				TLSClientHello: &tlsutils.ClientHello{
					Version: tls.VersionTLS13,
				},
				OpenEvent: &OpenEvent{
					Local: qnet.NetAddr{
						IP:   net.ParseIP("5.6.7.8"),
						Port: 5678,
					},
					Remote: qnet.NetAddr{
						IP:   net.ParseIP("1.2.3.4"),
						Port: 1234,
					},
					Source:       Client,
					SocketType:   SocketType_UDP,
					IsRedirected: false,
				},
				CloseEvent: &CloseEvent{
					WrBytes: 2000,
					RdBytes: 1000,
				},
				process:  process,
				Protocol: Protocol_HTTP2,
			}),
		)
	})
}
