package client

import (
	"net/http"
	"time"

	"github.com/qpoint-io/qtap/pkg/telemetry"
)

// NewHttpClient creates a custom HTTP client with optimized settings for service-to-service communication.
// It configures:
// - Connection pooling with controlled limits to prevent resource exhaustion
// - Timeouts at various levels (idle, response, TLS, overall) to ensure responsive behavior
// - Keep-alive settings for connection reuse to reduce latency
//
// The client is specifically tuned for internal service calls with:
// - Limited concurrent connections (10 per host) to prevent overwhelming downstream services
// - Short idle timeouts (5s) to free up resources quickly
// - Conservative overall timeout (30s) to prevent hung operations
func NewHttpClient() *http.Client {
	// Clone the default transport
	t := http.DefaultTransport.(*http.Transport).Clone()

	// Connection pool settings
	t.IdleConnTimeout = 5 * time.Second
	t.MaxIdleConnsPerHost = 100
	t.MaxConnsPerHost = 0 // Limit total connections per host

	// Timeout settings
	t.ResponseHeaderTimeout = 10 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second

	// Keep-alive settings
	t.DisableKeepAlives = false // Enable keep-alives for connection reuse
	t.MaxIdleConns = 0          // Overall connection pool limit

	// TLS settings
	t.TLSHandshakeTimeout = 5 * time.Second

	cl := &http.Client{
		Transport: t,
		Timeout:   30 * time.Second, // Overall request timeout
	}
	telemetry.InstrumentHTTPClient(cl)
	return cl
}
