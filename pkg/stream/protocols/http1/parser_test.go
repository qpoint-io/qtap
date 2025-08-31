package http1

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStreamParser_ProcessBytes(t *testing.T) {
	t.Run("http requests", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantReq *http.Request
			wantErr bool
		}{
			{
				name: "simple GET request",
				input: "GET /path HTTP/1.1\r\n" +
					"Host: example.com\r\n" +
					"x-custom-header: custom-header-value\r\n" +
					"\r\n",
				wantReq: &http.Request{
					Method: http.MethodGet,
					URL: &url.URL{
						Path: "/path",
					},
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"X-Custom-Header": []string{"custom-header-value"},
					},
				},
			},
			{
				name: "POST request with body",
				input: "POST /submit HTTP/1.1\r\n" +
					"Host: example.com\r\n" +
					"Content-Length: 11\r\n" +
					"\r\n" +
					"hello world",
				wantReq: &http.Request{
					Method: http.MethodPost,
					URL: &url.URL{
						Path: "/submit",
					},
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"Content-Length": []string{"11"},
					},
				},
			},
			{
				name: "kubelet request",
				input: `GET /health HTTP/1.1
Host: 10.44.0.13:8080
User-Agent: kube-probe/1.30
Accept: */*
Connection: close


`,
				wantReq: &http.Request{
					Method: http.MethodGet,
					URL: &url.URL{
						Path: "/health",
					},
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"User-Agent": []string{"kube-probe/1.30"},
						"Accept":     []string{"*/*"},
						"Connection": []string{"close"},
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var gotReq *http.Request
				var gotBody []byte
				var bodyDone bool

				headerHandler := func(msg *http.Request, noBody bool) {
					gotReq = msg
				}

				bodyHandler := func(chunk []byte, done bool) {
					if chunk != nil {
						gotBody = append(gotBody, chunk...)
					}
					if done {
						bodyDone = true
					}
				}

				parser := NewStreamParser(t.Context(), zap.NewNop(), headerHandler, bodyHandler)
				go func() {
					_ = parser.parse()
				}()

				_, err := parser.Write([]byte(tt.input))
				require.NoError(t, err)

				// Give the parser goroutine time to process
				time.Sleep(100 * time.Millisecond)

				require.Equal(t, tt.wantReq.Method, gotReq.Method)
				require.Equal(t, tt.wantReq.URL.Path, gotReq.URL.Path)
				require.Equal(t, tt.wantReq.Header, gotReq.Header)

				if strings.HasPrefix(tt.wantReq.Method, "POST") {
					require.True(t, bodyDone)
					require.Equal(t, "hello world", string(gotBody))
				}
			})
		}
	})

	t.Run("http responses", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			wantResp *http.Response
			wantErr  bool
			wantBody string
		}{
			{
				name: "simple 200 response (no body)",
				input: "HTTP/1.1 200 OK\r\n" +
					"Content-Type: text/plain\r\n" +
					"\r\n",
				wantResp: &http.Response{
					Status:     "200 " + http.StatusText(http.StatusOK),
					StatusCode: http.StatusOK,
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"Content-Type": []string{"text/plain"},
					},
				},
			},
			{
				name: "404 response with body",
				input: "HTTP/1.1 404 Not Found\r\n" +
					"Content-Type: text/plain\r\n" +
					"Content-Length: 9\r\n" +
					"\r\n" +
					"not found",
				wantResp: &http.Response{
					Status:     "404 " + http.StatusText(http.StatusNotFound),
					StatusCode: http.StatusNotFound,
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"Content-Type":   []string{"text/plain"},
						"Content-Length": []string{"9"},
					},
				},
			},
			{
				name: "200 response with chunked encoding",
				input: "HTTP/1.1 200 OK\r\n" +
					"Content-Type: text/plain\r\n" +
					"Transfer-Encoding: chunked\r\n" +
					"\r\n" +
					"7\r\n" +
					"Mozilla\r\n" +
					"9\r\n" +
					"Developer\r\n" +
					"7\r\n" +
					"Network\r\n" +
					"0\r\n" +
					"\r\n",
				wantResp: &http.Response{
					Status:     "200 " + http.StatusText(http.StatusOK),
					StatusCode: http.StatusOK,
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"Content-Type": []string{"text/plain"},
					},
				},
				wantBody: "MozillaDeveloperNetwork",
			},
			{
				name: "kubelet response",
				input: `HTTP/1.1 200 OK
Date: Fri, 03 Jan 2025 01:03:36 GMT
Content-Length: 2
Content-Type: text/plain; charset=utf-8
Connection: close

OK
`,
				wantResp: &http.Response{
					Status:     "200 " + http.StatusText(http.StatusOK),
					StatusCode: http.StatusOK,
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header: http.Header{
						"Date":           []string{"Fri, 03 Jan 2025 01:03:36 GMT"},
						"Content-Length": []string{"2"},
						"Content-Type":   []string{"text/plain; charset=utf-8"},
					},
				},
				wantBody: "OK",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var gotResp *http.Response
				var gotBody []byte
				var bodyDone bool

				headerHandler := func(msg *http.Response, noBody bool) {
					gotResp = msg
				}

				bodyHandler := func(chunk []byte, done bool) {
					if chunk != nil {
						gotBody = append(gotBody, chunk...)
					}
					if done {
						bodyDone = true
					}
				}

				parser := NewStreamParser(t.Context(), zap.NewNop(), headerHandler, bodyHandler)
				go func() {
					_ = parser.parse()
				}()

				_, err := parser.Write([]byte(tt.input))
				require.NoError(t, err)

				// Give the parser goroutine time to process
				time.Sleep(100 * time.Millisecond)

				require.Equal(t, tt.wantResp.StatusCode, gotResp.StatusCode)
				require.Equal(t, tt.wantResp.Status, gotResp.Status)
				require.Equal(t, tt.wantResp.Header, gotResp.Header)

				if tt.wantResp.StatusCode == http.StatusNotFound {
					require.True(t, bodyDone)
					require.Equal(t, "not found", string(gotBody))
				} else if tt.wantResp.StatusCode == http.StatusOK && tt.wantBody != "" {
					require.True(t, bodyDone)
					require.Equal(t, tt.wantBody, string(gotBody))
				}
			})
		}
	})
}
