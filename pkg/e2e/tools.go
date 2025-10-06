//go:build e2e

package e2e

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
)

type HTTPServerFunc func(machineIP string, handler http.Handler) (*httptest.Server, error)

// NewHTTP11OnlyTestServer creates an HTTP/1.1-only test server bound to a specific IP
// This is useful when you need to test HTTP/1.1-specific behavior or ensure
// compatibility with legacy clients
func NewHTTP11OnlyTestServer(machineIP string, handler http.Handler) (*httptest.Server, error) {
	// Create the unstarted server with your handler
	server := httptest.NewUnstartedServer(handler)

	// Create a listener on the specific machine IP with a random port
	listener, err := net.Listen("tcp", machineIP+":0")
	if err != nil {
		return nil, err
	}

	// Replace the server's listener with your custom one
	server.Listener = listener
	server.Config.Addr = listener.Addr().String()

	// Configure TLS to only support HTTP/1.1
	// This prevents automatic HTTP/2 upgrade during TLS negotiation
	server.TLS = &tls.Config{
		// Only advertise HTTP/1.1 support - no HTTP/2
		NextProtos: []string{"http/1.1"},
	}

	// Start with TLS (to match the HTTP/2 server's security level)
	server.StartTLS()

	// Configure the test client to use HTTP/1.1 only
	server.Client().Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			// Trust the test certificate
			InsecureSkipVerify: true,
			// Explicitly set HTTP/1.1 only for the client too
			NextProtos: []string{"http/1.1"},
		},
		// Explicitly disable HTTP/2 attempts
		ForceAttemptHTTP2: false,
		// You might want to disable keep-alives for certain tests
		// DisableKeepAlives: true,
	}

	return server, nil
}

// NewPlainHTTP11TestServer creates a non-TLS HTTP/1.1 server
// Use this when you don't need TLS and want to ensure HTTP/1.1 behavior
func NewPlainHTTP11TestServer(machineIP string, handler http.Handler) (*httptest.Server, error) {
	// Create the unstarted server
	server := httptest.NewUnstartedServer(handler)

	// Create a listener on the specific machine IP
	listener, err := net.Listen("tcp", machineIP+":0")
	if err != nil {
		return nil, err
	}

	// Configure the server with your custom listener
	server.Listener = listener
	server.Config.Addr = listener.Addr().String()

	// Start without TLS - this guarantees HTTP/1.1 since HTTP/2 requires TLS
	server.Start()

	// The default client will automatically use HTTP/1.1 for non-TLS connections
	// No special configuration needed

	return server, nil
}

// NewHTTP2OnlyTestServer creates an HTTP/2-only test server bound to a specific IP
func NewHTTP2OnlyTestServer(machineIP string, handler http.Handler) (*httptest.Server, error) {
	// Create the unstarted server with your handler
	server := httptest.NewUnstartedServer(handler)

	// Create a listener on the specific machine IP with a random port
	listener, err := net.Listen("tcp", machineIP+":0")
	if err != nil {
		return nil, err
	}

	// Replace the server's listener with your custom one
	server.Listener = listener
	server.Config.Addr = listener.Addr().String()

	// Configure TLS to only support HTTP/2
	server.TLS = &tls.Config{
		// This is the crucial part: only advertise HTTP/2 support
		NextProtos: []string{"h2"},
	}

	// Start with TLS (required for HTTP/2)
	server.StartTLS()

	// Configure the test client to properly connect to the server
	server.Client().Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			// Trust the test certificate
			InsecureSkipVerify: true,
		},
		// Ensure the client attempts HTTP/2
		ForceAttemptHTTP2: true,
	}

	return server, nil
}
