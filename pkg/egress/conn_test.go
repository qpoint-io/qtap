package egress

import (
	"bytes"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockConn implements net.Conn interface for testing
type mockConn struct {
	readData  []byte
	writeData bytes.Buffer
	closed    bool
}

func (m *mockConn) Read(p []byte) (n int, err error) {
	if len(m.readData) == 0 {
		return 0, io.EOF
	}
	n = copy(p, m.readData)
	m.readData = m.readData[n:]
	return n, nil
}

func (m *mockConn) Write(p []byte) (n int, err error) {
	return m.writeData.Write(p)
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }
func (m *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9090} }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestBufferedConn(t *testing.T) {
	testData := []byte("Hello, World!")
	mock := &mockConn{readData: testData}

	// Create BufferedConn
	bc := newBufferedConn(mock)

	// Test Peek
	peeked, err := bc.Peek(5)
	require.NoError(t, err)
	assert.Equal(t, []byte("Hello"), peeked)

	// Test that original data is still there after peek
	peeked, err = bc.Peek(5)
	require.NoError(t, err)
	assert.Equal(t, []byte("Hello"), peeked)

	// Test Read
	buf := make([]byte, len(testData))
	n, err := bc.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, testData, buf)

	// Test embedded net.Conn methods
	assert.Equal(t, mock.LocalAddr(), bc.LocalAddr())
	assert.Equal(t, mock.RemoteAddr(), bc.RemoteAddr())

	// Test Close
	err = bc.Close()
	require.NoError(t, err)
	assert.True(t, mock.closed)
}

func TestForwarderConn(t *testing.T) {
	testData := []byte("Hello, World!")
	mock := &mockConn{readData: testData}
	logger := zap.NewNop()

	// Create test metadata
	meta := &connectionMeta{
		Family:             syscall.AF_INET,
		OriginalDstAddr:    &net.TCPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 443},
		TlsTerminationSafe: true,
		Cookie:             12345,
		Logger:             logger,
	}

	// Create ForwarderConn
	fc, err := newConn(mock, meta)
	require.NoError(t, err)

	// Test GetMeta
	retrievedMeta := fc.getMeta()
	assert.Equal(t, meta, retrievedMeta)

	// Test UpdateMeta
	newMeta := &connectionMeta{
		Family:             syscall.AF_INET,
		OriginalDstAddr:    &net.TCPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 443},
		TlsTerminationSafe: false,
		Cookie:             67890,
		Logger:             logger,
	}
	fc.updateMeta(newMeta)
	assert.Equal(t, newMeta, fc.getMeta())

	// Test inherited BufferedConn functionality
	// Test Peek
	peeked, err := fc.Peek(5)
	require.NoError(t, err)
	assert.Equal(t, []byte("Hello"), peeked)

	// Test Read
	buf := make([]byte, len(testData))
	n, err := fc.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, testData, buf)

	// Test Close
	err = fc.Close()
	require.NoError(t, err)
	assert.True(t, mock.closed)
}

func TestForwarderConnWithNilMeta(t *testing.T) {
	mock := &mockConn{readData: []byte("test")}

	// Test with nil metadata
	fc, err := newConn(mock, nil)
	require.NoError(t, err)
	assert.Nil(t, fc.getMeta())
}

func TestBufferedConnLargeRead(t *testing.T) {
	// Test with data larger than default buffer size
	largeData := make([]byte, 32*1024) // 32KB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	mock := &mockConn{readData: largeData}
	bc := newBufferedConn(mock)

	// Read in chunks
	readData := make([]byte, 0, len(largeData))
	buf := make([]byte, 1024)
	for {
		n, err := bc.Read(buf)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		readData = append(readData, buf[:n]...)
	}

	assert.Equal(t, largeData, readData)
}

func TestBufferedConnPeekBoundary(t *testing.T) {
	testData := []byte("Hello, World!")
	mock := &mockConn{readData: testData}
	bc := newBufferedConn(mock)

	// Test peeking more bytes than available
	peeked, err := bc.Peek(100)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected error: %v", err)
	}
	assert.Equal(t, testData, peeked)

	// Test peeking at exact boundary
	peeked, err = bc.Peek(len(testData))
	require.NoError(t, err)
	assert.Equal(t, testData, peeked)

	// Test peeking zero bytes
	peeked, err = bc.Peek(0)
	require.NoError(t, err)
	assert.Empty(t, peeked)
}
