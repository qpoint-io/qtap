//go:build e2e

package e2e

// import (
// 	"net/http"
// 	"net/http/httptest"
// 	"testing"

// 	"github.com/qpoint-io/qtap/pkg/e2e"
// 	"github.com/stretchr/testify/require"
// 	"go.uber.org/zap"
// )

// func TestE2ETools(t *testing.T) {
// 	t.Helper()
// 	t.Run("Plain HTTP/1.1 Server", func(t *testing.T) {
// 		testWithHTTPServerProtocol(t, "HTTP/1.1", e2e.NewPlainHTTP11TestServer)
// 	})
// 	t.Run("HTTP/1.1 Only Server", func(t *testing.T) {
// 		testWithHTTPServerProtocol(t, "HTTP/1.1", e2e.NewHTTP11OnlyTestServer)
// 	})
// 	t.Run("HTTP/2 Only Server", func(t *testing.T) {
// 		testWithHTTPServerProtocol(t, "HTTP/2.0", e2e.NewHTTP2OnlyTestServer)
// 	})
// }

// func testWithHTTPServerProtocol(t *testing.T, protocol string, serverFunc func(machineIP string, handler http.Handler) (*httptest.Server, error)) {
// 	ctx := e2ectx.TestCtx(t)

// 	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		// Log the protocol to verify we're using the correct protocol
// 		ctx.L.Info("⚙️ handling request", zap.String("protocol", r.Proto))

// 		w.Header().Set("Content-Type", "application/json")
// 		w.WriteHeader(http.StatusOK)
// 		w.Write([]byte(`{"message": "Hello from test", "status": "success"}`))
// 	})

// 	machineIP := ctx.MachineIP().String()

// 	server, err := serverFunc(machineIP, handler)
// 	require.NoError(t, err, "Failed to create %s server", protocol)
// 	defer server.Close()

// 	// Make a test request
// 	resp, err := server.Client().Get(server.URL)
// 	require.NoError(t, err, "Failed to make request")
// 	defer resp.Body.Close()

// 	// Verify we're using the correct protocol
// 	require.Equal(t, protocol, resp.Proto, "Expected %s, got %s", protocol, resp.Proto)
// }
