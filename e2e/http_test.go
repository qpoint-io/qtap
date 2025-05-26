//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
