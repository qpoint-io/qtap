package connection

import (
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
)

var (
	connOpenTotal   = metrics.NewCounterVec("open_total", "The total number of connections opened", []string{"remote_addr", "remote_port", "direction"})
	connCloseTotal  = metrics.NewCounterVec("close_total", "The total number of connections closed", []string{"remote_addr", "remote_port", "direction"})
	connActiveTotal = metrics.NewGaugeVec("active_total", "The total number of active connections", []string{"remote_addr", "remote_port", "direction"})
	connDuration    = metrics.NewHistogramVec("duration_ms", "The duration of connections in milliseconds",
		[]string{"remote_addr", "remote_port"},
		1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000, 250000, 500000, 1000000, 1800000,
	)
	connBytesSentTotal    = metrics.NewCounterVec("bytes_sent_total", "The total number of bytes sent", []string{"remote_addr", "remote_port", "direction"})
	connBytesRecvTotal    = metrics.NewCounterVec("bytes_recv_total", "The total number of bytes received", []string{"remote_addr", "remote_port", "direction"})
	connProtocolTotal     = metrics.NewCounterVec("protocol_total", "The total number of connections by protocol", []string{"remote_addr", "remote_port", "direction", "protocol"})
	connTLSHandshakeTotal = metrics.NewCounterVec("tls_handshake_total", "The total number of TLS handshakes", []string{"remote_addr", "remote_port", "direction", "sni", "version"})
)
