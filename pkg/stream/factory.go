package stream

import (
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/plugins"
	dnsStream "github.com/qpoint-io/qtap/pkg/stream/protocols/dns"
	"github.com/qpoint-io/qtap/pkg/stream/protocols/http1"
	"github.com/qpoint-io/qtap/pkg/stream/protocols/http2"
	redisStream "github.com/qpoint-io/qtap/pkg/stream/protocols/redis"
	"go.uber.org/zap"
)

type StreamFactory struct {
	// logger
	logger *zap.Logger

	// dns manager
	dnsManager *dns.DNSManager

	// plugin manager
	pluginManager *plugins.Manager
}

type StreamFactoryOpt func(*StreamFactory)

func SetDnsManager(manager *dns.DNSManager) StreamFactoryOpt {
	return func(m *StreamFactory) {
		m.dnsManager = manager
	}
}

func SetPluginManager(manager *plugins.Manager) StreamFactoryOpt {
	return func(m *StreamFactory) {
		m.pluginManager = manager
	}
}

func NewStreamFactory(logger *zap.Logger, opts ...StreamFactoryOpt) *StreamFactory {
	m := &StreamFactory{
		logger: logger,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *StreamFactory) OnConnection(conn *connection.Connection) connection.StreamProcessor {
	logger := conn.Logger()

	// parse dns streams
	if conn.Protocol == connection.Protocol_DNS && conn.OpenEvent.Source == connection.Client && m.dnsManager != nil {
		return dnsStream.NewDNSStream(conn.Context(), logger, conn, m.dnsManager)
	}

	// handle mongodb streams (one day we will parse them)
	if conn.Protocol == connection.Protocol_MONGODB {
		logger.Debug("MongoDB connection detected - protocol parsing not implemented")
		return nil
	}

	// parse redis streams
	if conn.Protocol == connection.Protocol_REDIS {
		return redisStream.NewStream(conn.Context(), logger, conn)
	}

	// parse http streams
	if conn.Protocol == connection.Protocol_HTTP1 || conn.Protocol == connection.Protocol_HTTP2 {
		// extract the domain
		domain := conn.Domain()

		// if the domain does not have a stack and no default stack is set, skip it
		if _, exists := m.pluginManager.GetDomainStack(domain, "http"); !exists {
			return nil
		}

		// parse http/1 streams
		if conn.Protocol == connection.Protocol_HTTP1 {
			return http1.NewHTTPStream(conn.Context(), domain, logger, conn,
				http1.SetPluginManager(m.pluginManager),
			)
		}

		// parse http/2 streams
		if conn.Protocol == connection.Protocol_HTTP2 {
			return http2.NewHTTPStream(conn.Context(), domain, logger, conn,
				http2.SetPluginManager(m.pluginManager),
			)
		}
	}

	return nil
}
