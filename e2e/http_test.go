//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
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

// This test verifies the TLS probes merged from Qpoint decode language traffic.
func TestLanguageTLSProbes(t *testing.T) {
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

	suite, err := e2e.NewTestSuite("HTTP TLS Probes").
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
			require.Len(t, events.Requests, 1)

			expectedProbe := map[e2e.Language]string{
				e2e.NodeJS: "nodetls",
				e2e.Go:     "gotls",
				e2e.Java:   "javassl",
			}[ctx.TestCase.Language]
			require.True(t, events.Connections[0].TLSIntrospected)
			require.Contains(t, events.Connections[0].TLSProbeTypesDetected, expectedProbe)

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

func wget(ctx *e2e.TestContext, args ...string) e2e.ExecResult {
	return ctx.Exec("wget", append([]string{"-q", "-O", "-", "--timeout=3"}, args...)...)
}

// TestHTTP_WgetMsgPeek is a regression test for QPT-754 / QPT-988.
//
// wget uses MSG_PEEK flag in recvfrom() syscalls during plaintext HTTP reads.
// This causes qtap's eBPF probes to see the same data bytes twice — once for
// the peek and once for the actual read — corrupting the HTTP parser state so
// that response headers leak into the captured body.
//
// This test will FAIL until the MSG_PEEK fix is applied (filtering peek
// events in the eBPF recvfrom hooks). The curl subtest serves as a baseline
// to prove the test harness itself works correctly.
func TestHTTP_WgetMsgPeek(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	expectedBody := `{"message":"hello","status":"ok"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, expectedBody)
	}))
	defer server.Close()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS

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
		t.Run("curl_baseline", func(t *testing.T) {
			// curl does a single large recvfrom without MSG_PEEK — no corruption
			result := curl(ctx, server.URL)
			require.NoError(t, result.Err)
			require.Equal(t, expectedBody, result.Output)

			events := result.AwaitEvents(1)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

			require.Len(t, events.Requests, 1)
			assert.Equal(t, "GET", events.Requests[0].Method)

			require.Len(t, events.Artifacts, 1)
			assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, events.Artifacts[0].Type)

			var tx httpcapture.HttpTransaction
			err := json.Unmarshal(events.Artifacts[0].Data, &tx)
			require.NoError(t, err)
			assert.Equal(t, "GET", tx.Request.Method)
			assert.Equal(t, "application/json", tx.Response.ContentType)
			assert.Equal(t, expectedBody, string(tx.Response.Body))
		})

		t.Run("wget_msgpeek", func(t *testing.T) {
			// wget uses recvfrom(..., MSG_PEEK) then split read() calls.
			// TODO(QPT-754): This assertion on the captured body will fail until
			// the eBPF recvfrom hooks filter out MSG_PEEK events. The wget output
			// itself is correct, but the captured artifact body will be corrupted
			// (headers leaking into body).
			result := wget(ctx, server.URL)
			require.NoError(t, result.Err)
			require.Equal(t, expectedBody, result.Output)

			events := result.AwaitEvents(1)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

			require.Len(t, events.Requests, 1)
			assert.Equal(t, "GET", events.Requests[0].Method)

			require.Len(t, events.Artifacts, 1)
			assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, events.Artifacts[0].Type)

			var tx httpcapture.HttpTransaction
			err := json.Unmarshal(events.Artifacts[0].Data, &tx)
			require.NoError(t, err)
			assert.Equal(t, "GET", tx.Request.Method)
			assert.Equal(t, "application/json", tx.Response.ContentType)
			// This is the key assertion — captured body should match the actual
			// response. With the MSG_PEEK bug, headers leak into this field.
			assert.Equal(t, expectedBody, string(tx.Response.Body))
		})
	})
}

// TestHTTP_WgetMsgPeekLargeBody is a more aggressive regression test for
// QPT-754 / QPT-988. Uses a large response body to force wget's MSG_PEEK
// split read pattern: peek 511 bytes, read headers, read body separately.
// Small responses may fit in a single read and not trigger the corruption.
func TestHTTP_WgetMsgPeekLargeBody(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// Build a body large enough to force wget's split read pattern.
	// wget peeks 511 bytes with MSG_PEEK, then reads headers separately,
	// then reads body in another call. We need headers + body > 511 bytes
	// so the body spans a separate read() call after the peek.
	bodyData := map[string]interface{}{
		"message": "This is a larger response body designed to trigger wget MSG_PEEK split reads",
		"padding": strings.Repeat("abcdefghij", 50), // 500 bytes of padding
		"status":  "ok",
		"items":   []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"},
	}
	expectedBodyBytes, err := json.Marshal(bodyData)
	require.NoError(t, err)
	expectedBody := string(expectedBodyBytes)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "test-msgpeek-large")
		w.WriteHeader(http.StatusOK)
		w.Write(expectedBodyBytes)
	}))
	defer server.Close()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS

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
		t.Run("curl_baseline", func(t *testing.T) {
			result := curl(ctx, server.URL)
			require.NoError(t, result.Err)
			require.Equal(t, expectedBody, result.Output)

			events := result.AwaitEvents(1)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

			require.Len(t, events.Artifacts, 1)
			var tx httpcapture.HttpTransaction
			err := json.Unmarshal(events.Artifacts[0].Data, &tx)
			require.NoError(t, err)
			assert.Equal(t, "GET", tx.Request.Method)
			assert.Equal(t, expectedBody, string(tx.Response.Body),
				"curl captured body should match exactly")
		})

		t.Run("wget_msgpeek_large", func(t *testing.T) {
			result := wget(ctx, server.URL)
			require.NoError(t, result.Err)
			require.Equal(t, expectedBody, result.Output,
				"wget stdout should have correct body (wget itself works fine)")

			events := result.AwaitEvents(1)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

			require.Len(t, events.Artifacts, 1)
			var tx httpcapture.HttpTransaction
			err := json.Unmarshal(events.Artifacts[0].Data, &tx)
			require.NoError(t, err)
			assert.Equal(t, "GET", tx.Request.Method)
			// This is the key assertion — with MSG_PEEK bug, the captured body
			// will contain leaked headers instead of (or mixed with) the actual
			// JSON body. QPT-754 fix should make this pass.
			assert.Equal(t, expectedBody, string(tx.Response.Body),
				"captured body should match actual response (fails with MSG_PEEK bug: headers leak into body)")
		})
	})
}

func TestHTTP_ReverseProxy(t *testing.T) {
	ctx := e2ectx.TestCtx(t)
	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}, func(t *testing.T) {
		echoCtx := e2ectx.TestCtx(t)
		echoServer := echoHTTPServer(echoCtx)
		echoCtx.L.Warn("echoServer", zap.String("url", echoServer.String()))

		t.Run("caddy", func(t *testing.T) {
			caddyCtx := e2ectx.TestCtx(t)
			caddy, err := testcontainers.Run(
				caddyCtx, "caddy:2-alpine",
				testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
					hc.PidMode = "host"
				}),
				testcontainers.WithLogConsumerConfig(newLogOnFailureCollector(t)),
				testcontainers.WithExposedPorts("80/tcp"),
				testcontainers.WithWaitStrategy(wait.ForHTTP("/").WithStartupTimeout(10*time.Second)),
				testcontainers.WithEnv(map[string]string{
					"QPOINT_TAGS": "ctxid:" + caddyCtx.ID,
				}),
				testcontainers.WithFiles(testcontainers.ContainerFile{
					ContainerFilePath: "/etc/caddy/Caddyfile",
					FileMode:          0o644,
					Reader: strings.NewReader(`
						:80
						respond / "Hello from proxy!" 200
						reverse_proxy /proxy* ` + echoServer.String() + ` {
							transport http {
								keepalive off
							}
						}`,
					),
				}),
			)
			require.NoError(t, err)

			caddyIP, err := caddy.ContainerIP(ctx)
			require.NoError(t, err)

			// wait until we scan the caddy process
			inspect, err := caddy.Inspect(ctx)
			require.NoError(t, err)
			caddyPID := inspect.State.Pid
			caddyCtx.L.Debug("waiting for caddy process to be ready", zap.Int("pid", caddyPID))
			err = caddyCtx.WaitForProcess(e2e.NewProcessKey(caddy.ID, caddyPID), 15*time.Second)
			require.NoError(t, err)
			caddyCtx.L.Debug("caddy process is ready")
			time.Sleep(2 * time.Second)

			// proxy-terminated request
			// ----
			curlCtx := e2ectx.TestCtx(t)
			httpReq := curl(curlCtx, caddyIP, "-H", "Connection: close")
			require.NoError(t, httpReq.Err)
			require.Equal(t, "Hello from proxy!", httpReq.Output)

			events := curlCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn := events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Contains(t, conn.Source.(*eventstore.ConnectionEndpointLocal).Exe, "curl")
			assert.Equal(t, caddyIP, conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())

			require.Len(t, events.Requests, 1)
			req := events.Requests[0]
			assert.Equal(t, "GET", req.Method)
			assert.Equal(t, "text/plain; charset=utf-8", req.ContentType)

			// proxy-to-backend request
			// ----
			curlCtx = e2ectx.TestCtx(t)
			httpReq = curl(curlCtx, caddyIP+`/proxy`, "-H", "Connection: close")
			require.NoError(t, httpReq.Err)
			require.Equal(t, "hello world\n", httpReq.Output)

			// test curl connection
			events = curlCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn = events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Equal(t, caddyIP, conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())

			require.Len(t, events.Requests, 1)
			req = events.Requests[0]
			assert.Equal(t, "GET", req.Method)
			assert.Equal(t, "text/plain; charset=utf-8", req.ContentType)

			// test proxy -> backend connection
			events = caddyCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn = events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Equal(t, caddyIP, conn.Source.(*eventstore.ConnectionEndpointLocal).Address.IP.String())
			assert.Equal(t, echoServer.Hostname(), conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())
		})

		t.Run("traefik", func(t *testing.T) {
			traefikCtx := e2ectx.TestCtx(t)
			traefik, err := testcontainers.Run(
				traefikCtx, "traefik:v3",
				testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
					hc.PidMode = "host"
				}),
				testcontainers.WithLogConsumerConfig(newLogOnFailureCollector(t)),
				testcontainers.WithExposedPorts("80/tcp"),
				testcontainers.WithWaitStrategy(wait.ForHTTP("/ping").WithPort("80/tcp").WithStartupTimeout(10*time.Second)),
				testcontainers.WithEnv(map[string]string{
					"QPOINT_TAGS": "ctxid:" + traefikCtx.ID,
				}),
				testcontainers.WithFiles(
					testcontainers.ContainerFile{
						ContainerFilePath: "/etc/traefik/traefik.yml",
						FileMode:          0o644,
						Reader: strings.NewReader(`
api:
  insecure: true
ping:
  entryPoint: web
entryPoints:
  web:
    address: ":80"
providers:
  file:
    filename: /etc/traefik/dynamic.yml
`,
						),
					},
					testcontainers.ContainerFile{
						ContainerFilePath: "/etc/traefik/dynamic.yml",
						FileMode:          0o644,
						Reader: strings.NewReader(`
http:
  routers:
    proxy:
      rule: "Path(\"/proxy\")"
      service: backend
      priority: 1
  services:
    backend:
      loadBalancer:
        servers:
          - url: "` + echoServer.String() + `"
        serversTransport: no-keepalive
  serversTransports:
    no-keepalive:
      maxIdleConnsPerHost: -1
      forwardingTimeouts:
        idleConnTimeout: -1
`,
						),
					},
				),
			)
			require.NoError(t, err)

			traefikIP, err := traefik.ContainerIP(ctx)
			require.NoError(t, err)

			// wait until we scan the traefik process
			inspect, err := traefik.Inspect(ctx)
			require.NoError(t, err)
			traefikPID := inspect.State.Pid
			traefikCtx.L.Debug("waiting for traefik process to be ready", zap.Int("pid", traefikPID))
			err = traefikCtx.WaitForProcess(e2e.NewProcessKey(traefik.ID, traefikPID), 15*time.Second)
			require.NoError(t, err)
			traefikCtx.L.Debug("traefik process is ready")
			time.Sleep(2 * time.Second)

			// proxy-terminated request
			// ----
			curlCtx := e2ectx.TestCtx(t)
			httpReq := curl(curlCtx, traefikIP+"/ping", "-H", "Connection: close")
			require.NoError(t, httpReq.Err)
			require.Equal(t, "OK", httpReq.Output)

			events := curlCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn := events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Contains(t, conn.Source.(*eventstore.ConnectionEndpointLocal).Exe, "curl")
			assert.Equal(t, traefikIP, conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())

			require.Len(t, events.Requests, 1)
			req := events.Requests[0]
			assert.Equal(t, "GET", req.Method)

			// proxy-to-backend request
			// ----
			curlCtx = e2ectx.TestCtx(t)
			httpReq = curl(curlCtx, traefikIP+`/proxy`, "-H", "Connection: close")
			require.NoError(t, httpReq.Err)
			require.Equal(t, "hello world\n", httpReq.Output)

			// test curl connection
			events = curlCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn = events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Equal(t, traefikIP, conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())

			require.Len(t, events.Requests, 1)
			req = events.Requests[0]
			assert.Equal(t, "GET", req.Method)
			assert.Equal(t, "text/plain; charset=utf-8", req.ContentType)

			// test proxy -> backend connection
			events = traefikCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn = events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Equal(t, traefikIP, conn.Source.(*eventstore.ConnectionEndpointLocal).Address.IP.String())
			assert.Equal(t, echoServer.Hostname(), conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())
		})

		t.Run("envoy", func(t *testing.T) {
			envoyCtx := e2ectx.TestCtx(t)
			envoy, err := testcontainers.Run(
				envoyCtx, "envoyproxy/envoy:v1.31-latest",
				testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
					hc.PidMode = "host"
				}),
				testcontainers.WithLogConsumerConfig(newLogOnFailureCollector(t)),
				testcontainers.WithExposedPorts("80/tcp"),
				testcontainers.WithWaitStrategy(wait.ForHTTP("/").WithPort("80/tcp").WithStartupTimeout(10*time.Second)),
				testcontainers.WithEnv(map[string]string{
					"QPOINT_TAGS": "ctxid:" + envoyCtx.ID,
				}),
				testcontainers.WithFiles(testcontainers.ContainerFile{
					ContainerFilePath: "/etc/envoy/envoy.yaml",
					FileMode:          0o644,
					Reader: strings.NewReader(`
static_resources:
  listeners:
  - name: listener_http
    address:
      socket_address:
        address: 0.0.0.0
        port_value: 80
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: ingress_http
          route_config:
            name: local_route
            virtual_hosts:
            - name: local_service
              domains: ["*"]
              routes:
              - match: { prefix: "/proxy" }
                route:
                  cluster: echo_service
                  timeout: 0s
              - match: { prefix: "/" }
                direct_response:
                  status: 200
                  body:
                    inline_string: "Hello from proxy!"
          http_filters:
          - name: envoy.filters.http.router
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
  clusters:
  - name: echo_service
    connect_timeout: 1s
    type: STATIC
    lb_policy: ROUND_ROBIN
    common_http_protocol_options:
      max_requests_per_connection: 1
    load_assignment:
      cluster_name: echo_service
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: ` + echoServer.Hostname() + `
                port_value: 5678
`),
				}),
				testcontainers.WithCmd("-c", "/etc/envoy/envoy.yaml"),
			)
			require.NoError(t, err)

			envoyIP, err := envoy.ContainerIP(ctx)
			require.NoError(t, err)

			// wait until we scan the envoy process
			inspect, err := envoy.Inspect(ctx)
			require.NoError(t, err)
			envoyPID := inspect.State.Pid
			envoyCtx.L.Debug("waiting for envoy process to be ready", zap.Int("pid", envoyPID))
			err = envoyCtx.WaitForProcess(e2e.NewProcessKey(envoy.ID, envoyPID), 15*time.Second)
			require.NoError(t, err)
			envoyCtx.L.Debug("envoy process is ready")
			time.Sleep(2 * time.Second)

			// proxy-terminated request
			// ----
			curlCtx := e2ectx.TestCtx(t)
			httpReq := curl(curlCtx, envoyIP, "-H", "Connection: close")
			require.NoError(t, httpReq.Err)
			require.Equal(t, "Hello from proxy!", httpReq.Output)

			events := curlCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn := events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Contains(t, conn.Source.(*eventstore.ConnectionEndpointLocal).Exe, "curl")
			assert.Equal(t, envoyIP, conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())

			require.Len(t, events.Requests, 1)
			req := events.Requests[0]
			assert.Equal(t, "GET", req.Method)

			// proxy-to-backend request
			// ----
			curlCtx = e2ectx.TestCtx(t)
			httpReq = curl(curlCtx, envoyIP+`/proxy`, "-H", "Connection: close")
			require.NoError(t, httpReq.Err)
			require.Equal(t, "hello world\n", httpReq.Output)

			// test curl connection
			events = curlCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn = events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Equal(t, envoyIP, conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())

			require.Len(t, events.Requests, 1)
			req = events.Requests[0]
			assert.Equal(t, "GET", req.Method)
			assert.Equal(t, "text/plain; charset=utf-8", req.ContentType)

			// test proxy -> backend connection
			events = envoyCtx.Events(1)
			require.Len(t, events.Connections, 1)
			conn = events.Connections[0]
			assert.Equal(t, eventstore.Direction_EgressInternal, conn.Direction)
			assert.Equal(t, eventstore.L7Protocol_HTTP1, conn.L7Protocol)
			assert.Equal(t, envoyIP, conn.Source.(*eventstore.ConnectionEndpointLocal).Address.IP.String())
			assert.Equal(t, echoServer.Hostname(), conn.Destination.(*eventstore.ConnectionEndpointRemote).Address.IP.String())
		})
	})
}

type logOnFailureCollector struct {
	t    *testing.T
	logs bytes.Buffer
}

func newLogOnFailureCollector(t *testing.T) *testcontainers.LogConsumerConfig {
	c := &logOnFailureCollector{t: t}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("container logs:\n%s\n", c.logs.String())
		}
	})
	return &testcontainers.LogConsumerConfig{
		Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
		Consumers: []testcontainers.LogConsumer{c},
	}
}

func (lc *logOnFailureCollector) Accept(l testcontainers.Log) {
	lc.logs.Write(l.Content)
}
