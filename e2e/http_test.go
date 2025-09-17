//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/e2e/babel"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestHTTP(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	ctx.WithConfig(t, nil, func(t *testing.T) {
		// exec a process that makes an http request
		example := ctx.Exec("curl", "http://example.com")
		require.NoError(t, example.Err)

		// ensure we captured the connection
		events := example.AwaitEvents(1)
		conn := events.Connections[0]
		assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)
	})
}

func TestHTTP_SSE(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// setup http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf("Event %d", i))
			time.Sleep(1 * time.Second)
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false

		var pluginConfYaml yaml.Node
		err := pluginConfYaml.Encode(&httpcapture.HttpCaptureConfig{
			Level:  httpcapture.CaptureLevelFull,
			Format: httpcapture.OutputFormatJSON,
		})
		require.NoError(t, err)

		c.Stacks[c.Tap.Http.Stack] = config.Stack{
			Plugins: []config.Plugin{
				{
					Type:   string(httpcapture.PluginTypeHttpCapture),
					Config: pluginConfYaml,
				},
				{
					Type: string(report.PluginTypeReport),
				},
			},
		}
	}, func(t *testing.T) {
		expectedBody := `data: Event 0

data: Event 1

data: Event 2

`

		httpReq := curl(ctx, "--max-time", "5", "--no-buffer", server.URL)
		require.NoError(t, httpReq.Err)
		require.Equal(t, expectedBody, httpReq.Output)

		events := httpReq.AwaitEvents(1)

		// conn
		assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

		// req
		require.Len(t, events.Requests, 1)
		req := events.Requests[0]
		assert.Equal(t, "text/event-stream", req.ContentType)

		// captured artifacts
		require.Len(t, events.Artifacts, 1)
		artifact := events.Artifacts[0]
		assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, artifact.Type)
		var transaction httpcapture.HttpTransaction
		err := json.Unmarshal(artifact.Data, &transaction)
		require.NoError(t, err)
		assert.Equal(t, "GET", transaction.Request.Method)
		assert.Equal(t, "text/event-stream", transaction.Response.ContentType)
		assert.Equal(t, expectedBody, string(transaction.Response.Body))
	})
}

func curl(ctx *e2e.TestContext, args ...string) e2e.ExecResult {
	return ctx.Exec("curl", append([]string{"--silent", "--show-error", "--max-time", "2.5"}, args...)...)
}

// testBabelHTTPRequest runs a test against a specific babel image
func testBabelHTTPRequest(t *testing.T, image babel.HTTPRequestImage) {
	t.Helper()
	ctx := e2ectx.TestCtx(t)

	// Setup HTTP server
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Hello from test", "status": "success"}`))
	}))

	l, err := net.Listen("tcp", ctx.MachineIP().String()+":0")
	if err != nil {
		t.Fatalf("httptest: failed to listen on a port: %v", err)
	}
	server.Listener = l
	server.Config.Addr = l.Addr().String()
	server.Start()
	defer server.Close()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false

		var pluginConfYaml yaml.Node
		err := pluginConfYaml.Encode(&httpcapture.HttpCaptureConfig{
			Level:  httpcapture.CaptureLevelFull,
			Format: httpcapture.OutputFormatJSON,
		})
		require.NoError(t, err)

		c.Stacks[c.Tap.Http.Stack] = config.Stack{
			Plugins: []config.Plugin{
				{
					Type:   string(httpcapture.PluginTypeHttpCapture),
					Config: pluginConfYaml,
				},
				{
					Type: string(report.PluginTypeReport),
				},
			},
		}
	}, func(t *testing.T) {
		t.Logf("server URL: %s", server.URL)

		// create our request
		request := babel.NewHTTPRequest(image, server.URL).
			WithMethod("GET").
			WithHTTPVersion("1.1").
			WithTimeout(10 * time.Second).
			WithOutputFormat("json")

		result := ctx.Do(request)
		require.NoError(t, result.Err)
		require.Equal(t, 0, result.Code, "Container should exit successfully, logs: %s", result.Output)

		ctx.L.Info("✅ result", zap.String("ID", result.ID), zap.Int("code", result.Code), zap.String("output", result.Output), zap.Error(result.Err))

		events := result.AwaitEvents(1)
		ctx.L.Info("✅ events", zap.Any("events", events))

		// Validate connection
		require.Len(t, events.Connections, 1)
		t.Logf("connection: %#v", events.Connections[0])
		conn := events.Connections[0]
		assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)
		assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)

		// Validate HTTP request
		require.Len(t, events.Requests, 1)
		req := events.Requests[0]
		assert.Equal(t, "application/json", req.ContentType)

		// Validate captured artifacts
		require.Len(t, events.Artifacts, 1)
		artifact := events.Artifacts[0]
		assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, artifact.Type)

		var transaction httpcapture.HttpTransaction
		err = json.Unmarshal(artifact.Data, &transaction)
		require.NoError(t, err)
		assert.Equal(t, "GET", transaction.Request.Method)
		assert.Equal(t, "application/json", transaction.Response.ContentType)
		assert.Contains(t, string(transaction.Response.Body), "Hello from test")
	})
}

func TestPython_Requests(t *testing.T) {
	images := babel.AllPythonImages()

	for _, image := range images {
		image := image // capture loop variable
		t.Run(string(image), func(t *testing.T) {
			testBabelHTTPRequest(t, image)
		})
	}
}

func TestRuby_Requests(t *testing.T) {
	images := babel.AllRubyImages()

	for _, image := range images {
		image := image // capture loop variable
		t.Run(string(image), func(t *testing.T) {
			testBabelHTTPRequest(t, image)
		})
	}
}

// func TestBabel_AllImages(t *testing.T) {
// 	images := babel.AllImages()

// 	for _, image := range images {
// 		image := image // capture loop variable
// 		t.Run(string(image), func(t *testing.T) {
// 			testBabelHTTPRequest(t, image)
// 		})
// 	}
// }
