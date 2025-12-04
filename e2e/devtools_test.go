//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/advbet/sseclient"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDevTools(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// create a test server and register the devtools routes
	mux := http.NewServeMux()
	devtoolsManager.RegisterRoutes(mux, "")
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	streamReady := make(chan struct{})
	sseClCtx, sseClCancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(sseClCancel)
	streamClosed := make(chan struct{})

	var wg sync.WaitGroup
	go func() {
		// wait until stream ingestion is ready
		<-streamReady

		t.Log("making curl request")
		req1 := curl(ctx, "http://example.com")
		require.NoError(t, req1.Err, "curl request failed")

		// wait until the e2e stores pick up the curl request
		events := req1.AwaitEvents(1)
		require.Len(t, events.Connections, 1)
		require.Len(t, events.Requests, 1)
	}()

	// start a goroutine to consume events from the devtools SSE stream
	var eventChans = map[string]chan struct{}{
		"process.started":          make(chan struct{}, 1),
		"process.stopped":          make(chan struct{}, 1),
		"request.http_transaction": make(chan struct{}, 1),
		// we don't watch for `connection.opened` events because the reporter service is configured
		// to only report connections once they are closed
		"connection.closed": make(chan struct{}, 1),
	}

	go func() {
		defer func() {
			// close all event channels to unblock the wait group
			for _, ch := range eventChans {
				close(ch)
			}
			close(streamClosed)
		}()

		client := sseclient.New(server.URL+"/api/events", "")
		// override the http transport to include the body
		// as the sseclient package hardcodes http.NoBody on the request
		transport := &httpTransportOverrideBody{
			transport: client.HTTPClient.Transport,
			body: io.NopCloser(bytes.NewReader([]byte(`{
				"filter_topics": ["connection", "request", "process"],
				"skip_process_snapshot": true
			}`))),
		}
		client.HTTPClient.Transport = transport

		var curlPid atomic.Int32
		err := client.Start(sseClCtx, func(e *sseclient.Event) error {
			ctx.L.Debug("🌊 received SSE event", zap.String("event", e.Event))
			switch e.Event {
			case "system.connected":
				ctx.L.Debug("🌊 stream connected")
				// signal that the stream is ready so we can make a curl request
				streamReady <- struct{}{}
				// the curl request should then produce the following events:
			case "process.started":
				data, err := unmarshalDevToolsEvent[struct {
					PID    int    `json:"pid"`
					Binary string `json:"binary"`
				}](e.Data)
				if err != nil {
					return fmt.Errorf("unmarshalling process.started event: %w", err)
				}

				// TODO: update the helper `curl()` / `e2e.Exec()` to return the process object
				// so we can be more accurate and check the pid.
				if data.Binary == "curl" {
					ctx.L.Debug("🌊 curl process started", zap.Int("pid", data.PID))
					eventChans[e.Event] <- struct{}{}
					curlPid.Store(int32(data.PID))
				}

			case "process.stopped":
				data, err := unmarshalDevToolsEvent[struct {
					PID int `json:"pid"`
				}](e.Data)
				if err != nil {
					return fmt.Errorf("unmarshalling process.stopped event: %w", err)
				}

				expectedPid := int(curlPid.Load())
				if data.PID == expectedPid {
					ctx.L.Debug("🌊 curl process stopped", zap.Int("pid", data.PID))
					eventChans[e.Event] <- struct{}{}
				} else {
					ctx.L.Error("🌊 unexpected pid", zap.Int("pid", data.PID), zap.Int("expected", expectedPid))
				}

			case "request.http_transaction":
				data, err := unmarshalDevToolsEvent[struct {
					Data eventstore.Artifact `json:"data"`
				}](e.Data)
				if err != nil {
					return fmt.Errorf("unmarshalling request.http_transaction event: %w", err)
				}

				if data.Data.Type == eventstore.ArtifactType_HTTPTransaction {
					// TODO: we should be more selective here. We should probably check the connection id.
					// Perhaps we should buffer all events until the e2e event store picks up the events and
					// then go through and compare the events.
					ctx.L.Debug("🌊 received request.http_transaction event")
					eventChans[e.Event] <- struct{}{}
				}

			case "connection.closed":
				data, err := unmarshalDevToolsEvent[struct {
					Data EventstoreConnection `json:"data"`
				}](e.Data)
				if err != nil {
					return fmt.Errorf("unmarshalling connection.closed event: %w", err)
				}

				if slices.Contains(data.Data.Tags["ctxid"], ctx.ID) {
					ctx.L.Debug("🌊 received connection.closed event for ctxid", zap.String("ctxid", ctx.ID))
					eventChans[e.Event] <- struct{}{}
				}
			default:
				ctx.L.Warn("🌊 dropping unexpected event", zap.String("event", e.Event))
			}
			return nil
		}, func(err error) error {
			return err
		})
		require.NoError(t, err)
	}()

	// wait for all expected events to be received
	for event, ch := range eventChans {
		wg.Add(1)
		go func(event string, ch chan struct{}) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				require.NoError(t, ctx.Err())
			case _, ok := <-ch:
				if !ok {
					ctx.L.Error("🌊 channel was closed before we received the event", zap.String("event", event))
					t.Fail()
					return
				}
				ctx.L.Debug("🌊 received event", zap.String("event", event))
			}
		}(event, ch)
	}

	ctx.L.Debug("🌊 waiting for events to be received")
	wg.Wait()
	// stop the SSE stream
	sseClCancel()

	ctx.L.Debug("🌊 waiting for stream to close")
	<-streamClosed
}

type httpTransportOverrideBody struct {
	transport http.RoundTripper
	body      io.ReadCloser
}

func (h *httpTransportOverrideBody) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Body = h.body
	if h.transport == nil {
		h.transport = http.DefaultTransport
	}
	return h.transport.RoundTrip(req)
}

func unmarshalDevToolsEvent[T any](sseData []byte) (T, error) {
	ev, err := unmarshalJSON[struct {
		TS   string          `json:"ts"`
		Data json.RawMessage `json:"data"`
	}](sseData)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("unmarshalling outer devtools event: %w", err)
	}

	v, err := unmarshalJSON[T](ev.Data)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("unmarshalling inner json: %w", err)
	}
	return v, nil
}

func unmarshalJSON[T any](data []byte) (T, error) {
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}

type EventstoreConnection eventstore.Connection

// UnmarshalJSON implements custom JSON unmarshaling for Connection to handle
// the ConnectionEndpoint interface fields.
func (c *EventstoreConnection) UnmarshalJSON(data []byte) error {
	// Intermediate struct with RawMessage for interface fields
	aux := &struct {
		Source      json.RawMessage `json:"source,omitempty"`
		Destination json.RawMessage `json:"destination,omitempty"`
		*eventstore.Connection
	}{
		Connection: (*eventstore.Connection)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Unmarshal Source
	if aux.Source != nil {
		endpoint, err := unmarshalConnectionEndpoint(aux.Source)
		if err != nil {
			return fmt.Errorf("unmarshaling source: %w", err)
		}
		c.Source = endpoint
	}

	// Unmarshal Destination
	if aux.Destination != nil {
		endpoint, err := unmarshalConnectionEndpoint(aux.Destination)
		if err != nil {
			return fmt.Errorf("unmarshaling destination: %w", err)
		}
		c.Destination = endpoint
	}

	return nil
}

// unmarshalConnectionEndpoint determines the concrete type of ConnectionEndpoint
// by checking for fields unique to ConnectionEndpointLocal.
func unmarshalConnectionEndpoint(data []byte) (eventstore.ConnectionEndpoint, error) {
	// Check if this looks like a ConnectionEndpointLocal by checking for
	// fields that are unique to it (hostname, exe, user, userId, container)
	var test struct {
		Hostname  *string               `json:"hostname,omitempty"`
		Exe       *string               `json:"exe,omitempty"`
		User      *string               `json:"user,omitempty"`
		UserID    *uint                 `json:"userId,omitempty"`
		Container *eventstore.Container `json:"container,omitempty"`
	}

	if err := json.Unmarshal(data, &test); err != nil {
		return nil, err
	}

	// If any Local-specific fields are present, unmarshal as Local
	if test.Hostname != nil || test.Exe != nil || test.User != nil || test.UserID != nil || test.Container != nil {
		var local eventstore.ConnectionEndpointLocal
		if err := json.Unmarshal(data, &local); err != nil {
			return nil, err
		}
		return &local, nil
	}

	// Otherwise, unmarshal as Remote
	var remote eventstore.ConnectionEndpointRemote
	if err := json.Unmarshal(data, &remote); err != nil {
		return nil, err
	}
	return &remote, nil
}

