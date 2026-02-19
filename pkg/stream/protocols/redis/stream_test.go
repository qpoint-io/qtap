package redis

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// createTestStreamWithSource creates a stream with an observable logger and specified source
func createTestStreamWithSource(t *testing.T, source connection.Source) (*Stream, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	conn := &connection.Connection{
		OpenEvent: &connection.OpenEvent{
			Source: source,
		},
	}

	stream := NewStream(t.Context(), logger, conn)
	return stream, logs
}

// createTestStream creates a stream with Client source (default for most tests)
func createTestStream(t *testing.T) (*Stream, *observer.ObservedLogs) {
	return createTestStreamWithSource(t, connection.Client)
}

// --- isRequest/isResponse Tests ---

func TestIsRequest(t *testing.T) {
	tests := []struct {
		name   string
		source connection.Source
		dir    connection.Direction
		want   bool
	}{
		{"Client+Egress", connection.Client, connection.Egress, true},
		{"Client+Ingress", connection.Client, connection.Ingress, false},
		{"Server+Ingress", connection.Server, connection.Ingress, true},
		{"Server+Egress", connection.Server, connection.Egress, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRequest(tt.source, tt.dir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsResponse(t *testing.T) {
	tests := []struct {
		name   string
		source connection.Source
		dir    connection.Direction
		want   bool
	}{
		{"Client+Egress", connection.Client, connection.Egress, false},
		{"Client+Ingress", connection.Client, connection.Ingress, true},
		{"Server+Ingress", connection.Server, connection.Ingress, false},
		{"Server+Egress", connection.Server, connection.Egress, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isResponse(tt.source, tt.dir)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Stream Initialization Tests ---

func TestNewStream(t *testing.T) {
	stream, _ := createTestStream(t)

	assert.NotNil(t, stream)
	assert.NotNil(t, stream.requestParser)
	assert.NotNil(t, stream.responseParser)
	assert.NotNil(t, stream.pendingCommands)
	assert.False(t, stream.closed)
}

// --- Correlation Tests (Client Perspective) ---

func TestCorrelation_SimpleRequestResponse(t *testing.T) {
	stream, logs := createTestStream(t) // Client source

	// Send command (Client+Egress = request)
	cmdEvent := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"),
	}
	err := stream.Process(cmdEvent)
	require.NoError(t, err)

	// Command should be queued, only debug log
	infos := logs.FilterMessage("redis request/response").All()
	assert.Empty(t, infos)

	// Send response (Client+Ingress = response)
	respEvent := &connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("$3\r\nbar\r\n"),
	}
	err = stream.Process(respEvent)
	require.NoError(t, err)

	// Now should have correlated log
	infos = logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 1)

	entry := infos[0]
	assert.Equal(t, "GET", entry.ContextMap()["command"])
	assert.Equal(t, "bulk_string", entry.ContextMap()["response_type"])
	assert.Equal(t, int64(3), entry.ContextMap()["response_length"])
	assert.Equal(t, false, entry.ContextMap()["error"])
	assert.NotNil(t, entry.ContextMap()["latency"])
}

func TestCorrelation_Pipeline(t *testing.T) {
	stream, logs := createTestStream(t)

	// Send 3 pipelined commands
	pipeline := "*1\r\n$4\r\nPING\r\n" +
		"*2\r\n$3\r\nGET\r\n$1\r\na\r\n" +
		"*2\r\n$3\r\nGET\r\n$1\r\nb\r\n"

	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte(pipeline),
	}))

	// No correlated logs yet
	assert.Empty(t, logs.FilterMessage("redis request/response").All())

	// Send 3 responses
	responses := "+PONG\r\n$6\r\nvalueA\r\n$6\r\nvalueB\r\n"
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte(responses),
	}))

	// Should have 3 correlated entries
	infos := logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 3)

	// Verify FIFO correlation
	assert.Equal(t, "PING", infos[0].ContextMap()["command"])
	assert.Equal(t, "simple_string", infos[0].ContextMap()["response_type"])

	assert.Equal(t, "GET", infos[1].ContextMap()["command"])
	assert.Equal(t, "bulk_string", infos[1].ContextMap()["response_type"])

	assert.Equal(t, "GET", infos[2].ContextMap()["command"])
	assert.Equal(t, "bulk_string", infos[2].ContextMap()["response_type"])
}

func TestCorrelation_ErrorResponse(t *testing.T) {
	stream, logs := createTestStream(t)

	// Send command
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("*2\r\n$4\r\nINCR\r\n$3\r\nfoo\r\n"),
	}))

	// Send error response
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("-ERR value is not an integer\r\n"),
	}))

	// Should have warning-level log with error=true
	infos := logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 1)
	assert.Equal(t, true, infos[0].ContextMap()["error"])
	assert.Equal(t, "ERR value is not an integer", infos[0].ContextMap()["error_message"])
}

func TestCorrelation_ResponseWithoutCommand(t *testing.T) {
	stream, logs := createTestStream(t)

	// Send response without command
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("+OK\r\n"),
	}))

	// Should log warning about uncorrelated response
	warns := logs.FilterMessage("redis response without pending command").All()
	require.Len(t, warns, 1)

	// Should also log the uncorrelated response
	uncorrelated := logs.FilterMessage("redis response (uncorrelated)").All()
	require.Len(t, uncorrelated, 1)
}

func TestCorrelation_CloseWithPending(t *testing.T) {
	stream, logs := createTestStream(t)

	// Send commands without responses
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\n"),
	}))

	// Close stream
	stream.Close()

	// Should log warning about pending commands
	warns := logs.FilterMessage("redis stream closed with pending commands").All()
	require.Len(t, warns, 1)
	assert.Equal(t, int64(2), warns[0].ContextMap()["pending_count"])
}

// --- Correlation Tests (Server Perspective) ---

func TestCorrelation_ServerPerspective(t *testing.T) {
	stream, logs := createTestStreamWithSource(t, connection.Server)

	// Command arrives via Ingress (Server receiving from client)
	cmdEvent := &connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"),
	}
	err := stream.Process(cmdEvent)
	require.NoError(t, err)

	// Response via Egress (Server sending to client)
	respEvent := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("$3\r\nbar\r\n"),
	}
	err = stream.Process(respEvent)
	require.NoError(t, err)

	// Should have correlated log
	infos := logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 1)
	assert.Equal(t, "GET", infos[0].ContextMap()["command"])
}

func TestCorrelation_ServerPipeline(t *testing.T) {
	stream, logs := createTestStreamWithSource(t, connection.Server)

	// Multiple commands via Ingress
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("*1\r\n$4\r\nPING\r\n*2\r\n$3\r\nSET\r\n$1\r\na\r\n"),
	}))

	// Responses via Egress
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("+PONG\r\n+OK\r\n"),
	}))

	infos := logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 2)
	assert.Equal(t, "PING", infos[0].ContextMap()["command"])
	assert.Equal(t, "SET", infos[1].ContextMap()["command"])
}

// --- Chunked Data Tests ---

func TestProcessChunkedData(t *testing.T) {
	stream, logs := createTestStream(t)

	// First chunk - incomplete command
	event1 := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("*3\r\n$3\r\nSET"),
	}
	err := stream.Process(event1)
	require.NoError(t, err)

	// Second chunk - still incomplete
	event2 := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("\r\n$3\r\nfoo\r\n$3"),
	}
	err = stream.Process(event2)
	require.NoError(t, err)

	// Third chunk - completes the command
	event3 := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("\r\nbar\r\n"),
	}
	err = stream.Process(event3)
	require.NoError(t, err)

	// Command should be queued now (debug log)
	queued := logs.FilterMessage("redis command queued").All()
	require.Len(t, queued, 1)
	assert.Equal(t, "SET", queued[0].ContextMap()["command"])

	// Send response to complete correlation
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("+OK\r\n"),
	}))

	// Should have correlated log
	infos := logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 1)
	assert.Equal(t, "SET", infos[0].ContextMap()["command"])
}

// --- Auth/Sensitive Command Tests ---

func TestSanitizeAuthCommand(t *testing.T) {
	stream, logs := createTestStream(t)

	// AUTH command
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("*2\r\n$4\r\nAUTH\r\n$8\r\npassword\r\n"),
	}))

	// Send response
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      []byte("+OK\r\n"),
	}))

	infos := logs.FilterMessage("redis request/response").All()
	require.Len(t, infos, 1)
	assert.Equal(t, "AUTH", infos[0].ContextMap()["command"])

	// Check that args are redacted
	args := infos[0].ContextMap()["args"].([]any)
	require.Len(t, args, 1)
	assert.Equal(t, "[REDACTED]", args[0])
}

// --- Close/Lifecycle Tests ---

func TestProcessAfterClose(t *testing.T) {
	stream, logs := createTestStream(t)

	stream.Close()

	event := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      []byte("*1\r\n$4\r\nPING\r\n"),
	}

	err := stream.Process(event)
	require.NoError(t, err)

	// Should not log any command processing after close
	queued := logs.FilterMessage("redis command queued").All()
	assert.Empty(t, queued)
}

func TestCloseStream(t *testing.T) {
	stream, logs := createTestStream(t)

	assert.False(t, stream.Closed())

	stream.Close()

	assert.True(t, stream.Closed())

	closingLogs := logs.FilterMessage("closing redis stream").All()
	require.Len(t, closingLogs, 1)
}

// --- RESP3 Type Tests ---

func TestProcessRESP3TypesCorrelated(t *testing.T) {
	tests := []struct {
		name         string
		responseData string
		wantType     string
	}{
		{"null", "_\r\n", "null"},
		{"boolean true", "#t\r\n", "boolean"},
		{"double", ",3.14\r\n", "double"},
		{"big number", "(12345678901234567890\r\n", "big_number"},
		{"bulk error", "!10\r\nERR syntax\r\n", "bulk_error"},
		{"verbatim", "=8\r\ntxt:test\r\n", "verbatim"},
		{"map", "%1\r\n+key\r\n:1\r\n", "map"},
		{"set", "~2\r\n+a\r\n+b\r\n", "set"},
		{"push", ">2\r\n+pubsub\r\n+message\r\n", "push"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream, logs := createTestStream(t)

			// Send a command first
			require.NoError(t, stream.Process(&connection.DataEvent{
				Direction: connection.Egress,
				Data:      []byte("*1\r\n$4\r\nPING\r\n"),
			}))

			// Send the RESP3 response
			require.NoError(t, stream.Process(&connection.DataEvent{
				Direction: connection.Ingress,
				Data:      []byte(tt.responseData),
			}))

			// Should have correlated log with correct response type
			infos := logs.FilterMessage("redis request/response").All()
			require.Len(t, infos, 1)
			assert.Equal(t, tt.wantType, infos[0].ContextMap()["response_type"])
		})
	}
}

// --- Concurrent Access Tests ---

func TestConcurrentAccess(t *testing.T) {
	stream, _ := createTestStream(t)

	done := make(chan bool)

	// Concurrent request processing
	go func() {
		for range 100 {
			_ = stream.Process(&connection.DataEvent{
				Direction: connection.Egress,
				Data:      []byte("*1\r\n$4\r\nPING\r\n"),
			})
		}
		done <- true
	}()

	// Concurrent response processing
	go func() {
		for range 100 {
			_ = stream.Process(&connection.DataEvent{
				Direction: connection.Ingress,
				Data:      []byte("+PONG\r\n"),
			})
		}
		done <- true
	}()

	// Concurrent Closed checks
	go func() {
		for range 100 {
			_ = stream.Closed()
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done

	// Should complete without race conditions
	stream.Close()
	assert.True(t, stream.Closed())
}
