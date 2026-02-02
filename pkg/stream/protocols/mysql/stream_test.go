package mysql

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

func TestStreamClose(t *testing.T) {
	stream, _ := createTestStream(t)

	assert.False(t, stream.Closed())
	stream.Close()
	assert.True(t, stream.Closed())
}

// --- Request Processing Tests ---

func TestProcessRequestComQuery(t *testing.T) {
	stream, logs := createTestStream(t)

	// Build a COM_QUERY packet: "SELECT 1"
	query := "SELECT 1"
	payload := append([]byte{ComQuery}, []byte(query)...)
	packet := buildPacket(0, payload)

	event := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      packet,
	}

	err := stream.Process(event)
	require.NoError(t, err)

	// Verify command was queued
	assert.Len(t, stream.pendingCommands, 1)
	assert.Equal(t, ComQuery, stream.pendingCommands[0].CommandType)
	assert.Equal(t, query, stream.pendingCommands[0].Query)

	// Check debug log
	logEntries := logs.FilterMessage("mysql command queued")
	assert.GreaterOrEqual(t, logEntries.Len(), 1)
}

func TestProcessRequestComStmtPrepare(t *testing.T) {
	stream, _ := createTestStream(t)

	// Build a COM_STMT_PREPARE packet
	query := "SELECT * FROM users WHERE id = ?"
	payload := append([]byte{ComStmtPrepare}, []byte(query)...)
	packet := buildPacket(0, payload)

	event := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      packet,
	}

	err := stream.Process(event)
	require.NoError(t, err)

	assert.Len(t, stream.pendingCommands, 1)
	assert.Equal(t, ComStmtPrepare, stream.pendingCommands[0].CommandType)
	assert.Equal(t, query, stream.pendingCommands[0].Query)
}

func TestProcessMultipleRequests(t *testing.T) {
	stream, _ := createTestStream(t)

	queries := []string{"SELECT 1", "SELECT 2", "SELECT 3"}
	for _, q := range queries {
		payload := append([]byte{ComQuery}, []byte(q)...)
		packet := buildPacket(0, payload)
		event := &connection.DataEvent{
			Direction: connection.Egress,
			Data:      packet,
		}
		err := stream.Process(event)
		require.NoError(t, err)
	}

	assert.Len(t, stream.pendingCommands, 3)
	for i, cmd := range stream.pendingCommands {
		assert.Equal(t, queries[i], cmd.Query)
	}
}

// --- Response Processing Tests ---

func TestProcessResponseOK(t *testing.T) {
	stream, logs := createTestStream(t)

	// First, send a request
	query := "INSERT INTO test VALUES (1)"
	payload := append([]byte{ComQuery}, []byte(query)...)
	packet := buildPacket(0, payload)
	stream.Process(&connection.DataEvent{Direction: connection.Egress, Data: packet})

	// Now send OK response
	okPayload := []byte{
		0x00,       // OK header
		0x01,       // affected_rows = 1
		0x00,       // last_insert_id = 0
		0x02, 0x00, // status flags
		0x00, 0x00, // warnings
	}
	okPacket := buildPacket(1, okPayload)

	err := stream.Process(&connection.DataEvent{Direction: connection.Ingress, Data: okPacket})
	require.NoError(t, err)

	// Command should be dequeued
	assert.Len(t, stream.pendingCommands, 0)

	// Check correlated log
	logEntries := logs.FilterMessage("mysql request/response")
	assert.GreaterOrEqual(t, logEntries.Len(), 1)
}

func TestProcessResponseERR(t *testing.T) {
	stream, logs := createTestStream(t)

	// First, send a request
	query := "SELECT * FROM nonexistent"
	payload := append([]byte{ComQuery}, []byte(query)...)
	packet := buildPacket(0, payload)
	stream.Process(&connection.DataEvent{Direction: connection.Egress, Data: packet})

	// Now send ERR response
	errPayload := []byte{
		0xff,       // ERR header
		0x19, 0x04, // error_code = 1049
		'#',                     // sql_state_marker
		'4', '2', '0', '0', '0', // sql_state = 42000
		'U', 'n', 'k', 'n', 'o', 'w', 'n',
	}
	errPacket := buildPacket(1, errPayload)

	err := stream.Process(&connection.DataEvent{Direction: connection.Ingress, Data: errPacket})
	require.NoError(t, err)

	// Command should be dequeued
	assert.Len(t, stream.pendingCommands, 0)

	// Check warning log
	logEntries := logs.FilterMessage("mysql request/response")
	assert.GreaterOrEqual(t, logEntries.Len(), 1)
}

// --- Edge Cases ---

func TestProcessEmptyPacket(t *testing.T) {
	stream, _ := createTestStream(t)

	// Empty packet (just header, no payload)
	packet := []byte{0x00, 0x00, 0x00, 0x00}

	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      packet,
	})
	require.NoError(t, err)

	// No commands should be queued for empty packets
	assert.Len(t, stream.pendingCommands, 0)
}

func TestProcessIncompletePacket(t *testing.T) {
	stream, _ := createTestStream(t)

	// Incomplete packet (header says 10 bytes but only 5 provided)
	packet := []byte{0x0a, 0x00, 0x00, 0x00, 0x03, 'S', 'E', 'L'}

	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      packet,
	})
	require.NoError(t, err)

	// No commands should be queued yet (waiting for more data)
	assert.Len(t, stream.pendingCommands, 0)
}

func TestProcessServerHandshake(t *testing.T) {
	stream, logs := createTestStream(t)

	// Build a server handshake packet (protocol version 10)
	handshakePayload := []byte{
		0x0a,                                   // protocol version
		'5', '.', '7', '.', '0', 0x00,          // server version
		0x01, 0x00, 0x00, 0x00,                 // connection id
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', // auth data
		0x00,       // filler
		0xff, 0xff, // capability flags
		0x08,       // charset
		0x02, 0x00, // status
		0xff, 0xff, // capability upper
		0x15,       // auth length
	}
	packet := buildPacket(0, handshakePayload)

	err := stream.Process(&connection.DataEvent{Direction: connection.Ingress, Data: packet})
	require.NoError(t, err)

	// Should log handshake
	logEntries := logs.FilterMessage("mysql server handshake")
	assert.GreaterOrEqual(t, logEntries.Len(), 1)
}

func TestCloseWithPendingCommands(t *testing.T) {
	stream, logs := createTestStream(t)

	// Send requests without responses
	for i := 0; i < 3; i++ {
		query := "SELECT 1"
		payload := append([]byte{ComQuery}, []byte(query)...)
		packet := buildPacket(0, payload)
		stream.Process(&connection.DataEvent{Direction: connection.Egress, Data: packet})
	}

	assert.Len(t, stream.pendingCommands, 3)

	// Close the stream
	stream.Close()

	// Should warn about pending commands
	logEntries := logs.FilterMessage("mysql stream closed with pending commands")
	assert.Equal(t, 1, logEntries.Len())
}

func TestStreamThreadSafety(t *testing.T) {
	stream, _ := createTestStream(t)

	// Concurrent processing
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			query := "SELECT 1"
			payload := append([]byte{ComQuery}, []byte(query)...)
			packet := buildPacket(0, payload)
			stream.Process(&connection.DataEvent{Direction: connection.Egress, Data: packet})
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have processed all commands without panic
	assert.LessOrEqual(t, len(stream.pendingCommands), 10)
}

// --- Query Truncation Tests ---

func TestTruncateQuery(t *testing.T) {
	stream, _ := createTestStream(t)

	tests := []struct {
		name     string
		query    string
		wantLen  int
		wantTrim bool
	}{
		{"short query", "SELECT 1", 8, false},
		{"long query", "SELECT * FROM very_long_table_name WHERE condition = 'very long value that exceeds one hundred characters limit for truncation'", 100, true},
		{"with whitespace", "  SELECT 1  ", 8, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stream.truncateQuery(tt.query)
			assert.LessOrEqual(t, len(result), 100)
			if !tt.wantTrim {
				assert.Equal(t, tt.wantLen, len(result))
			}
		})
	}
}
