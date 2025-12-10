//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gopkg.in/yaml.v3"
)

func TestHTTP(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// setup http targetServer
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}, func(t *testing.T) {
		// exec a process that makes an http request
		example := curl(ctx, targetServer.URL)
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

func TestHTTP_Ingress(t *testing.T) {
	ctx := e2ectx.TestCtx(t)
	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_INGRESS
	}, func(t *testing.T) {
		// setup http server
		serverURL := echoHTTPServer(ctx)
		t.Logf("serverURL: %s", serverURL.String())

		// curl
		httpReq := curl(ctx, serverURL.String())
		require.NoError(t, httpReq.Err)
		require.Contains(t, httpReq.Output, "hello world")

		// test connection
		events := ctx.Events(1)
		require.Len(t, events.Connections, 1)
		assert.Equal(t, eventstore.Direction_Ingress, events.Connections[0].Direction)
		assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

		src := events.Connections[0].Source.(*eventstore.ConnectionEndpointRemote)
		dst := events.Connections[0].Destination.(*eventstore.ConnectionEndpointLocal)

		assert.NotEqual(t, serverURL.Hostname(), src.Address.IP.String())
		assert.NotZero(t, src.Address.Port)

		assert.Equal(t, serverURL.Hostname(), dst.Address.IP.String())
		assert.Equal(t, "5678", fmt.Sprint(dst.Address.Port))

		// test request
		require.Len(t, events.Requests, 1)
		req := events.Requests[0]
		assert.Equal(t, "GET", req.Method)
		assert.Equal(t, "text/plain; charset=utf-8", req.ContentType)
		assert.Equal(t,
			strings.TrimRight(serverURL.String(), "/"),
			strings.TrimRight(req.Url, "/"),
		)
	})
}

func echoHTTPServer(ctx *e2e.TestContext) *url.URL {
	t := ctx.T
	t.Helper()
	server, err := testcontainers.Run(
		ctx, "hashicorp/http-echo:latest",
		testcontainers.WithCmd("-text=hello world"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5678/tcp").WithStartupTimeout(10*time.Second).SkipExternalCheck()),
		testcontainers.WithEnv(map[string]string{
			"QPOINT_TAGS": "ctxid:" + ctx.ID,
		}),
	)
	t.Cleanup(func() {
		testcontainers.TerminateContainer(server)
	})
	require.NoError(t, err)

	containerIP, err := server.ContainerIP(ctx)
	require.NoError(t, err)

	return &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:5678", containerIP),
	}
}

// This test verifies that we capture the connection and subsequent HTTP
// request content and artifacts for languages that
// support TLS introspection within the Qtap opensource project.
func TestLanguages(t *testing.T) {
	configMut := func(c *config.Config) {
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
	}

	suite, err := e2e.NewTestSuite("HTTP Introspective").
		WithConfig(configMut).
		WithOS("alpine", "bullseye").
		WithLanguage(e2e.Python, "3.10.0", "3.12.0").
		WithLanguage(e2e.Ruby, "3.2.9", "3.3.9", "3.4.5").
		WithLanguage(e2e.PHP, "8.1", "8.2", "8.3").
		WithMethod(e2e.HTTPMethodGet).
		WithURL("/api/health").
		WithHTTPProtocols(e2e.HTTPProtocolHTTP1_1, e2e.HTTPProtocolHTTP2_0).
		WithBothTLSAndPlaintext().
		WithHeader("Content-Type", "application/json").
		WithReadinessHandshake("/tmp/readiness-signal", 15*time.Second).
		WithValidation(func(t *testing.T, ctx e2e.ValidationContext) error {
			events := ctx.TestContext.Events(1)
			require.Len(t, events.Connections, 1)

			switch ctx.TestCase.Request.Proto {
			case e2e.HTTPProtocolHTTP1_1:
				require.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)
			case e2e.HTTPProtocolHTTP2_0:
				require.Equal(t, eventstore.L7Protocol_HTTP2, events.Connections[0].L7Protocol)
			}

			// Validate HTTP request
			require.Len(t, events.Requests, 1)
			req := events.Requests[0]
			assert.Equal(t, "application/json", req.ContentType)

			// Validate captured artifacts
			require.Len(t, events.Artifacts, 1)
			artifact := events.Artifacts[0]
			assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, artifact.Type)

			var transaction httpcapture.HttpTransaction
			err := json.Unmarshal(artifact.Data, &transaction)
			require.NoError(t, err)
			assert.Equal(t, "GET", transaction.Request.Method)
			assert.Equal(t, "application/json", transaction.Response.ContentType)
			assert.Contains(t, string(transaction.Response.Body), "success")

			return nil
		}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, suite)

	// TODO(Jon): could this be rolled into the test suite?
	runner := &e2e.TestSuiteRunner{
		Suite:  suite,
		Logger: e2ectx.L,
	}
	runner.Run(t, e2ectx)
}

// This test verifies that we capture the connection for languages that do not
// support TLS introspection within the Qtap opensource project.
func TestLanguageNonIntrospective(t *testing.T) {
	configMut := func(c *config.Config) {
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
	}

	suite, err := e2e.NewTestSuite("HTTP NonIntrospective").
		WithConfig(configMut).
		WithOS("alpine").
		WithLanguage(e2e.NodeJS, "18.20.0", "22.16.0", "24.5.0").
		WithLanguage(e2e.Go, "1.22.0", "1.24.4", "1.25.1").
		WithLanguage(e2e.Java, "11", "17", "21").
		WithMethod(e2e.HTTPMethodGet).
		WithURL("/api/health").
		WithHTTPProtocols(e2e.HTTPProtocolHTTP1_1, e2e.HTTPProtocolHTTP2_0).
		WithTLSOnly().
		WithReadinessHandshake("/tmp/readiness-signal", 15*time.Second).
		WithValidation(func(t *testing.T, ctx e2e.ValidationContext) error {
			events := ctx.TestContext.Events(1)
			require.Len(t, events.Connections, 1)
			require.Len(t, events.Requests, 0)

			return nil
		}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, suite)

	// TODO(Jon): could this be rolled into the test suite?
	runner := &e2e.TestSuiteRunner{
		Suite:  suite,
		Logger: e2ectx.L,
	}
	runner.Run(t, e2ectx)
}

func curl(ctx *e2e.TestContext, args ...string) e2e.ExecResult {
	return ctx.Exec("curl", append([]string{"--silent", "--show-error", "--max-time", "2.5"}, args...)...)
}
