//go:build e2e

package e2e

// func TestHTTP(t *testing.T) {
// 	ctx := e2ectx.TestCtx(t)

// 	ctx.WithConfig(t, nil, func(t *testing.T) {
// 		// exec a process that makes an http request
// 		example := ctx.Exec("curl", "http://example.com")
// 		require.NoError(t, example.Err)

// 		// ensure we captured the connection
// 		events := example.AwaitEvents(1)
// 		conn := events.Connections[0]
// 		assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)
// 	})
// }

// func TestHTTP_SSE(t *testing.T) {
// 	ctx := e2ectx.TestCtx(t)

// 	// setup http server
// 	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		w.Header().Set("Content-Type", "text/event-stream")
// 		w.Header().Set("Cache-Control", "no-cache")
// 		w.Header().Set("Connection", "keep-alive")
// 		w.WriteHeader(http.StatusOK)

// 		for i := 0; i < 3; i++ {
// 			fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf("Event %d", i))
// 			time.Sleep(1 * time.Second)
// 			w.(http.Flusher).Flush()
// 		}
// 	}))
// 	defer server.Close()

// 	ctx.WithConfig(t, func(c *config.Config) {
// 		c.Tap.IgnoreLoopback = false

// 		var pluginConfYaml yaml.Node
// 		err := pluginConfYaml.Encode(&httpcapture.HttpCaptureConfig{
// 			Level:  httpcapture.CaptureLevelFull,
// 			Format: httpcapture.OutputFormatJSON,
// 		})
// 		require.NoError(t, err)

// 		c.Stacks[c.Tap.Http.Stack] = config.Stack{
// 			Plugins: []config.Plugin{
// 				{
// 					Type:   string(httpcapture.PluginTypeHttpCapture),
// 					Config: pluginConfYaml,
// 				},
// 				{
// 					Type: string(report.PluginTypeReport),
// 				},
// 			},
// 		}
// 	}, func(t *testing.T) {
// 		expectedBody := `data: Event 0

// data: Event 1

// data: Event 2

// `

// 		httpReq := curl(ctx, "--max-time", "5", "--no-buffer", server.URL)
// 		require.NoError(t, httpReq.Err)
// 		require.Equal(t, expectedBody, httpReq.Output)

// 		events := httpReq.AwaitEvents(1)

// 		// conn
// 		assert.Equal(t, eventstore.L7Protocol_HTTP1, events.Connections[0].L7Protocol)

// 		// req
// 		require.Len(t, events.Requests, 1)
// 		req := events.Requests[0]
// 		assert.Equal(t, "text/event-stream", req.ContentType)

// 		// captured artifacts
// 		require.Len(t, events.Artifacts, 1)
// 		artifact := events.Artifacts[0]
// 		assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, artifact.Type)
// 		var transaction httpcapture.HttpTransaction
// 		err := json.Unmarshal(artifact.Data, &transaction)
// 		require.NoError(t, err)
// 		assert.Equal(t, "GET", transaction.Request.Method)
// 		assert.Equal(t, "text/event-stream", transaction.Response.ContentType)
// 		assert.Equal(t, expectedBody, string(transaction.Response.Body))
// 	})
// }

// func curl(ctx *e2e.TestContext, args ...string) e2e.ExecResult {
// 	return ctx.Exec("curl", append([]string{"--silent", "--show-error", "--max-time", "2.5"}, args...)...)
// }

// // testBabelHTTPRequest runs a test against a specific babel image
// func testBabelHTTPRequest(t *testing.T, r *e2e.HTTPRequest) {
// 	t.Helper()
// 	ctx := e2ectx.TestCtx(t)

// 	ctx.WithConfig(t, func(c *config.Config) {
// 		c.Tap.IgnoreLoopback = false

// 		var pluginConfYaml yaml.Node
// 		err := pluginConfYaml.Encode(&httpcapture.HttpCaptureConfig{
// 			Level:  httpcapture.CaptureLevelFull,
// 			Format: httpcapture.OutputFormatJSON,
// 		})
// 		require.NoError(t, err)

// 		c.Stacks[c.Tap.Http.Stack] = config.Stack{
// 			Plugins: []config.Plugin{
// 				{
// 					Type:   string(httpcapture.PluginTypeHttpCapture),
// 					Config: pluginConfYaml,
// 				},
// 				{
// 					Type: string(report.PluginTypeReport),
// 				},
// 			},
// 		}
// 	}, func(t *testing.T) {
// 		result := ctx.Do(r)
// 		require.NoError(t, result.Err)
// 		require.Equal(t, 0, result.Code, "Container should exit successfully, logs: %s", result.Output)

// 		ctx.L.Info("✅ result", zap.String("ID", result.ID), zap.Int("code", result.Code), zap.String("output", result.Output), zap.Error(result.Err))

// 		events := result.AwaitEvents(1)
// 		ctx.L.Info("✅ events", zap.Any("events", events))

// 		// Validate connection
// 		require.Len(t, events.Connections, 1)
// 		conn := events.Connections[0]
// 		assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)

// 		var expectedL7Protocol eventstore.L7Protocol
// 		switch r.HTTPVersion {
// 		case "1.1":
// 			expectedL7Protocol = eventstore.L7Protocol_HTTP1
// 		case "2":
// 			expectedL7Protocol = eventstore.L7Protocol_HTTP2
// 		}
// 		assert.Equal(t, expectedL7Protocol, conn.L7Protocol)

// 		// Validate HTTP request
// 		require.Len(t, events.Requests, 1)
// 		req := events.Requests[0]
// 		assert.Equal(t, "application/json", req.ContentType)

// 		// Validate captured artifacts
// 		require.Len(t, events.Artifacts, 1)
// 		artifact := events.Artifacts[0]
// 		assert.Equal(t, eventstore.ArtifactType_HTTPTransaction, artifact.Type)

// 		var transaction httpcapture.HttpTransaction
// 		err := json.Unmarshal(artifact.Data, &transaction)
// 		require.NoError(t, err)
// 		assert.Equal(t, "GET", transaction.Request.Method)
// 		assert.Equal(t, "application/json", transaction.Response.ContentType)
// 		assert.Contains(t, string(transaction.Response.Body), "Hello from test")
// 	})
// }

// func TestPython_HTTP(t *testing.T) {
// 	ctx := e2ectx.TestCtx(t)
// 	images := e2e.AllPythonImages()

// 	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		ctx.L.Info("⚙️ handling request", zap.String("protocol", r.Proto), zap.String("url", r.URL.String()))

// 		w.Header().Set("Content-Type", "application/json")
// 		w.WriteHeader(http.StatusOK)
// 		w.Write([]byte(`{"message": "Hello from test", "status": "success"}`))
// 	})

// 	http1plainserver, err := e2e.NewPlainHTTP11TestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http1plainserver.Close()

// 	http1server, err := e2e.NewHTTP11OnlyTestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http1server.Close()

// 	http2server, err := e2e.NewHTTP2OnlyTestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http2server.Close()

// 	rb := e2e.BuildHTTPRequest().
// 		WithMethod("GET").
// 		WithTimeout(10 * time.Second).
// 		WithOutputFormat("json").
// 		WithVerbose().
// 		WithStartupDelay(100 * time.Millisecond)

// 	for _, image := range images {
// 		rb.WithImageURL(image.String())

// 		for _, client := range []string{"requests", "httpx", "aiohttp", "urllib3"} {
// 			t.Run(client, func(t *testing.T) {
// 				rb.WithClient(client)
// 				t.Run(image.TestName()+" HTTP/1.1 Plain", func(t *testing.T) {
// 					rb.WithURL(http1plainserver.URL).
// 						WithHTTPVersion("1.1")

// 					req, err := rb.Build()
// 					require.NoError(t, err)

// 					testBabelHTTPRequest(t, req)
// 				})
// 				t.Run(image.TestName()+" HTTP/1.1 TLS", func(t *testing.T) {
// 					if client == "aiohttp" {
// 						t.Skip("Skipping HTTP/1.1 TLS tests for aiohttp (unsupported)")
// 					}
// 					rb.WithURL(http1server.URL)
// 					rb.WithHTTPVersion("1.1")

// 					req, err := rb.Build()
// 					require.NoError(t, err)

// 					testBabelHTTPRequest(t, req)
// 				})
// 			})
// 		}
// 		t.Run(image.TestName()+" HTTP/2 TLS", func(t *testing.T) {
// 			rb.WithURL(http2server.URL).
// 				WithHTTPVersion("2").
// 				WithClient("httpx")

// 			req, err := rb.Build()
// 			require.NoError(t, err)

// 			testBabelHTTPRequest(t, req)
// 		})
// 	}
// }

// func TestRuby_HTTP(t *testing.T) {
// 	ctx := e2ectx.TestCtx(t)
// 	images := e2e.AllRubyImages()

// 	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		ctx.L.Info("⚙️ handling request", zap.String("protocol", r.Proto), zap.String("url", r.URL.String()))

// 		w.Header().Set("Content-Type", "application/json")
// 		w.WriteHeader(http.StatusOK)
// 		w.Write([]byte(`{"message": "Hello from test", "status": "success"}`))
// 	})

// 	http1plainserver, err := e2e.NewPlainHTTP11TestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http1plainserver.Close()

// 	http1server, err := e2e.NewHTTP11OnlyTestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http1server.Close()

// 	http2server, err := e2e.NewHTTP2OnlyTestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http2server.Close()

// 	rb := e2e.BuildHTTPRequest().
// 		WithMethod("GET").
// 		WithTimeout(10 * time.Second).
// 		WithOutputFormat("json").
// 		WithVerbose().
// 		WithStartupDelay(250 * time.Millisecond)

// 	for _, image := range images {
// 		rb.WithImageURL(image.String())

// 		t.Run(image.TestName()+" HTTP/1.1 Plain", func(t *testing.T) {
// 			rb.WithURL(http1plainserver.URL)
// 			rb.WithHTTPVersion("1.1")

// 			req, err := rb.Build()
// 			require.NoError(t, err)

// 			testBabelHTTPRequest(t, req)
// 		})
// 		t.Run(image.TestName()+" HTTP/1.1 TLS", func(t *testing.T) {
// 			rb.WithURL(http1server.URL)
// 			rb.WithHTTPVersion("1.1")

// 			req, err := rb.Build()
// 			require.NoError(t, err)

// 			testBabelHTTPRequest(t, req)
// 		})
// 		// t.Run(image.TestName()+" HTTP/2 TLS", func(t *testing.T) {
// 		// 	t.Skip("Skipping HTTP/2 TLS tests -- currently not supported by Babel containers")
// 		// 	rb.WithURL(http2server.URL)
// 		// 	rb.WithHTTPVersion("2")

// 		// 	req, err := rb.Build()
// 		// 	require.NoError(t, err)

// 		// 	testBabelHTTPRequest(t, req)
// 		// })
// 	}
// }

// func TestPHP_HTTP(t *testing.T) {
// 	ctx := e2ectx.TestCtx(t)
// 	images := e2e.AllPHPImages()

// 	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		ctx.L.Info("⚙️ handling request", zap.String("protocol", r.Proto), zap.String("url", r.URL.String()))

// 		w.Header().Set("Content-Type", "application/json")
// 		w.WriteHeader(http.StatusOK)
// 		w.Write([]byte(`{"message": "Hello from test", "status": "success"}`))
// 	})

// 	http1plainserver, err := e2e.NewPlainHTTP11TestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http1plainserver.Close()

// 	http1server, err := e2e.NewHTTP11OnlyTestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http1server.Close()

// 	http2server, err := e2e.NewHTTP2OnlyTestServer(e2ectx.MachineIP().String(), handler)
// 	require.NoError(t, err)
// 	defer http2server.Close()

// 	rb := e2e.BuildHTTPRequest().
// 		WithMethod("GET").
// 		WithTimeout(10 * time.Second).
// 		WithOutputFormat("json").
// 		WithVerbose().
// 		WithStartupDelay(500 * time.Millisecond)

// 	for _, image := range images {
// 		rb.WithImageURL(image.String())

// 		for _, client := range []string{"curl", "guzzle"} {
// 			t.Run(client, func(t *testing.T) {
// 				rb.WithClient(client)

// 				t.Run(image.TestName()+" HTTP/1.1 Plain", func(t *testing.T) {
// 					rb.WithURL(http1plainserver.URL)
// 					rb.WithHTTPVersion("1.1")

// 					req, err := rb.Build()
// 					require.NoError(t, err)

// 					testBabelHTTPRequest(t, req)
// 				})
// 				t.Run(image.TestName()+" HTTP/1.1 TLS", func(t *testing.T) {
// 					rb.WithURL(http1server.URL)
// 					rb.WithHTTPVersion("1.1")

// 					req, err := rb.Build()
// 					require.NoError(t, err)

// 					testBabelHTTPRequest(t, req)
// 				})
// 				t.Run(image.TestName()+" HTTP/2 TLS", func(t *testing.T) {
// 					rb.WithURL(http2server.URL)
// 					rb.WithHTTPVersion("2")

// 					req, err := rb.Build()
// 					require.NoError(t, err)

// 					testBabelHTTPRequest(t, req)
// 				})
// 			})
// 		}
// 	}
// }
