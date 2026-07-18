package l7detect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// Peeker is an interface that wraps the Peek method
type Peeker interface {
	Peek(n int) ([]byte, error)
}

var (
	http2PrefByteCount = 24
	http2ClientPreface = []byte(http2.ClientPreface)

	http1PrefByteCount  = 8
	http1MethodPrefixes = [][]byte{
		[]byte("GET"),
		[]byte("HTTP"),
		[]byte("POST"),
		[]byte("PUT"),
		[]byte("DELETE"),
		[]byte("PATCH"),
		[]byte("CONNECT"),
		[]byte("HEAD"),
		[]byte("OPTIONS"),
	}

	redisPrefByteCount = 4
)

// detectTLS checks if the first bytes of data indicate a TLS handshake
func DetectTLS(conn Peeker) (bool, error) {
	first, err := conn.Peek(3)
	if err != nil {
		return false, fmt.Errorf("peek error: %w", err)
	}

	// Check if it's a TLS handshake
	if len(first) >= 3 && first[0] == 0x16 && first[1] == 0x03 {
		return true, nil
	}

	return false, nil
}

// detectProtocol attempts to determine if the given reader contains an HTTP/1.x or HTTP/2 request/response
func DetectProtocol(logger *zap.Logger, conn Peeker) (connection.Protocol, error) {
	// Check for HTTP/2 connection preface
	if isHTTP2, err := isHTTP2(conn); isHTTP2 && err == nil {
		return connection.Protocol_HTTP2, nil
	} else if err != nil {
		logger.Error("failed to check for HTTP/2", zap.Error(err))
	}

	// Check for HTTP/1.x connection preface
	if isHTTP1, err := isHTTP1(conn); isHTTP1 && err == nil {
		return connection.Protocol_HTTP1, nil
	} else if err != nil {
		logger.Error("failed to check for HTTP/1", zap.Error(err))
	}

	// Check for Redis RESP protocol
	if isRedis, err := isRedis(conn); isRedis && err == nil {
		return connection.Protocol_REDIS, nil
	} else if err != nil {
		logger.Error("failed to check for Redis", zap.Error(err))
	}

	// If we can't determine the version, return unknown
	return connection.Protocol_UNKNOWN, nil
}

// Helper function to peek at the ClientHello message
func DetectClientHello(conn Peeker) (*tlsutils.ClientHello, error) {
	// Read the record header
	header, err := conn.Peek(5)
	if err != nil {
		return nil, err
	}

	// Check if it's a TLS handshake
	if header[0] != 0x16 {
		return nil, errors.New("not a TLS handshake")
	}

	// Get the length of the handshake message
	length := int(binary.BigEndian.Uint16(header[3:5]))

	// Peek the full handshake message
	data, err := conn.Peek(5 + length)
	if err != nil {
		return nil, err
	}

	ch, err := tlsutils.ParseClientHello(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client hello: %w", err)
	}

	return ch, nil
}

func isHTTP2(conn Peeker) (bool, error) {
	// Peek the first http 2 client preface bytes
	peek, err := conn.Peek(http2PrefByteCount)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, fmt.Errorf("failed to peek: %w", err)
	}

	// check for HTTP/2 connection preface
	if bytes.HasPrefix(peek, http2ClientPreface) {
		return true, nil
	}

	return false, nil
}

func isHTTP1(conn Peeker) (bool, error) {
	// Peek the first http 2 client preface bytes
	peek, err := conn.Peek(http1PrefByteCount)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, fmt.Errorf("failed to peek: %w", err)
	}

	// check for HTTP/1.x connection preface
	for _, prefix := range http1MethodPrefixes {
		if bytes.HasPrefix(peek, prefix) {
			return true, nil
		}
	}

	return false, nil
}

func isRedis(conn Peeker) (bool, error) {
	peek, err := conn.Peek(redisPrefByteCount)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, fmt.Errorf("failed to peek: %w", err)
	}

	// Check for RESP array command pattern: *<digit>
	// Redis commands are sent as RESP arrays: *<count>\r\n$<len>\r\n<cmd>...
	if len(peek) >= 2 && peek[0] == '*' && peek[1] >= '1' && peek[1] <= '9' {
		return true, nil
	}

	return false, nil
}
