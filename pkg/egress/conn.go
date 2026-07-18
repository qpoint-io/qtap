package egress

import (
	"bufio"
	"net"

	"go.uber.org/zap"
)

// bufferedConn is a wrapper around a net.Conn that provides a bufio.Reader for efficient reading.
// It also embeds the net.Conn to allow for easy access to the underlying connection's methods.
// This allows us to peek and read the underlying connection's data and still allow the reader to read the data again.
type bufferedConn struct {
	r        *bufio.Reader
	net.Conn // So that most methods are embedded
}

func newBufferedConn(c net.Conn) bufferedConn {
	return bufferedConn{bufio.NewReader(c), c}
}

func (b bufferedConn) Peek(n int) ([]byte, error) {
	return b.r.Peek(n)
}

func (b bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

type connectionMeta struct {
	Family             int
	OriginalDstAddr    *net.TCPAddr
	TlsTerminationSafe bool
	Cookie             uint64
	Logger             *zap.Logger
	FD                 int
}

// conn is a wrapper around a bufferedConn that includes connection metadata.
type conn struct {
	bufferedConn
	meta *connectionMeta
}

// newConn creates a new conn with the given net.Conn and logger.
func newConn(c net.Conn, meta *connectionMeta) (*conn, error) {
	bc := newBufferedConn(c)
	return &conn{bc, meta}, nil
}

// Example method to access metadata
func (fc *conn) getMeta() *connectionMeta {
	return fc.meta
}

// Example method to update metadata
func (fc *conn) updateMeta(newMeta *connectionMeta) {
	fc.meta = newMeta
}
