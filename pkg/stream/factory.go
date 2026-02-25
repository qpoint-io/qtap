package stream

import (
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/plugins"
	dnsStream "github.com/qpoint-io/qtap/pkg/stream/protocols/dns"
	"github.com/qpoint-io/qtap/pkg/stream/protocols/http1"
	"github.com/qpoint-io/qtap/pkg/stream/protocols/http2"
	kafkaStream "github.com/qpoint-io/qtap/pkg/stream/protocols/kafka"
	mysqlStream "github.com/qpoint-io/qtap/pkg/stream/protocols/mysql"
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
		domain := conn.Domain()

		// if the domain does not have a stack and no default stack is set, skip it
		if _, exists := m.pluginManager.GetDomainStack(domain, "redis"); !exists {
			return nil
		}

		return redisStream.NewStream(conn.Context(), logger, conn,
			redisStream.SetDomain(domain),
			redisStream.SetPluginManager(m.pluginManager),
		)
	}

	// parse mysql streams
	if conn.Protocol == connection.Protocol_MYSQL {
		domain := conn.Domain()

		// if the domain does not have a stack and no default stack is set, skip it
		if _, exists := m.pluginManager.GetDomainStack(domain, "mysql"); !exists {
			return nil
		}

		return mysqlStream.NewStream(conn.Context(), logger, conn,
			mysqlStream.SetDomain(domain),
			mysqlStream.SetPluginManager(m.pluginManager),
		)
	}

	// parse kafka streams
	if conn.Protocol == connection.Protocol_KAFKA {
		domain := conn.Domain()

		// if the domain does not have a stack and no default stack is set, skip it
		if _, exists := m.pluginManager.GetDomainStack(domain, "kafka"); !exists {
			return nil
		}

		return kafkaStream.NewStream(conn.Context(), logger, conn,
			kafkaStream.SetDomain(domain),
			kafkaStream.SetPluginManager(m.pluginManager),
		)
	}

	// parse http streams (gRPC uses the HTTP/2 parser — it's detected and reclassified inside)
	if conn.Protocol == connection.Protocol_HTTP1 || conn.Protocol == connection.Protocol_HTTP2 || conn.Protocol == connection.Protocol_GRPC {
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

		// parse http/2 streams (gRPC uses HTTP/2 transport and is parsed by the same handler)
		if conn.Protocol == connection.Protocol_HTTP2 || conn.Protocol == connection.Protocol_GRPC {
			return http2.NewHTTPStream(conn.Context(), domain, logger, conn,
				http2.SetPluginManager(m.pluginManager),
			)
		}
	}

	return nil
}
