package postgres

import (
	"encoding/binary"
	"errors"
)

var (
	// ErrIncomplete indicates more data is needed
	ErrIncomplete = errors.New("incomplete message")
	// ErrInvalidMessage indicates the message format is invalid
	ErrInvalidMessage = errors.New("invalid message format")
)

// Parser handles PostgreSQL protocol parsing for one direction
type Parser struct {
	buffer []byte
}

// NewParser creates a new PostgreSQL protocol parser
func NewParser() *Parser {
	return &Parser{
		buffer: make([]byte, 0, 4096),
	}
}

// Append adds data to the parser buffer
func (p *Parser) Append(data []byte) {
	p.buffer = append(p.buffer, data...)
}

// BufferLen returns the current buffer length
func (p *Parser) BufferLen() int {
	return len(p.buffer)
}

// Reset clears the parser buffer
func (p *Parser) Reset() {
	p.buffer = p.buffer[:0]
}

// ParseMessage attempts to parse the next complete PostgreSQL message.
// Standard messages: 1-byte type + 4-byte length + payload
// Returns ErrIncomplete if more data is needed.
func (p *Parser) ParseMessage() (*Message, error) {
	if len(p.buffer) < 5 {
		return nil, ErrIncomplete
	}

	// Read type byte
	msgType := p.buffer[0]

	// Read length (4 bytes, big-endian, includes itself but not type byte)
	length := binary.BigEndian.Uint32(p.buffer[1:5])

	// Sanity check length (max 1GB to prevent OOM)
	if length < 4 || length > 1<<30 {
		// Invalid length — skip this byte and try again
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidMessage
	}

	// Total bytes needed: 1 (type) + length
	totalLen := 1 + int(length)
	if len(p.buffer) < totalLen {
		return nil, ErrIncomplete
	}

	// Payload is everything after the 5-byte header (type + length)
	payloadLen := int(length) - 4
	msg := &Message{
		Type:    msgType,
		Length:  length,
		Payload: make([]byte, payloadLen),
	}
	copy(msg.Payload, p.buffer[5:5+payloadLen])

	// Consume the message
	p.buffer = p.buffer[totalLen:]

	return msg, nil
}

// ParseStartupMessage parses an untyped startup-phase message.
// Startup messages have NO type byte: just 4-byte length + payload.
// Used for StartupMessage, SSLRequest, CancelRequest, GSSENCRequest.
func (p *Parser) ParseStartupMessage() (*Message, error) {
	if len(p.buffer) < 4 {
		return nil, ErrIncomplete
	}

	// Read length (4 bytes, big-endian, includes itself)
	length := binary.BigEndian.Uint32(p.buffer[0:4])

	// Sanity check
	if length < 4 || length > 10000 {
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidMessage
	}

	if len(p.buffer) < int(length) {
		return nil, ErrIncomplete
	}

	// Payload is everything after the 4-byte length
	payloadLen := int(length) - 4
	msg := &Message{
		Type:    0, // untyped
		Length:  length,
		Payload: make([]byte, payloadLen),
	}
	copy(msg.Payload, p.buffer[4:4+payloadLen])

	// Consume
	p.buffer = p.buffer[int(length):]

	return msg, nil
}

// ParseSSLResponse parses the single-byte SSL response ('S' or 'N')
func (p *Parser) ParseSSLResponse() (byte, error) {
	if len(p.buffer) < 1 {
		return 0, ErrIncomplete
	}

	resp := p.buffer[0]
	p.buffer = p.buffer[1:]
	return resp, nil
}

// Helper functions for parsing message payloads

// ReadCString reads a null-terminated string from data starting at offset.
// Returns the string and the new offset (after the null byte).
func ReadCString(data []byte, offset int) (string, int) {
	for i := offset; i < len(data); i++ {
		if data[i] == 0 {
			return string(data[offset:i]), i + 1
		}
	}
	// No null terminator found — return what we have
	return string(data[offset:]), len(data)
}

// ParseQueryMessage extracts the SQL text from a Query ('Q') message payload.
func ParseQueryMessage(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	// SQL text is a null-terminated string
	sql, _ := ReadCString(payload, 0)
	return sql
}

// ParseParseMessage extracts the statement name and SQL text from a Parse ('P') message payload.
func ParseParseMessage(payload []byte) (stmtName string, sql string) {
	if len(payload) == 0 {
		return "", ""
	}
	pos := 0
	stmtName, pos = ReadCString(payload, pos)
	if pos < len(payload) {
		sql, _ = ReadCString(payload, pos)
	}
	return stmtName, sql
}

// ParseBindMessage extracts portal and statement names from a Bind ('B') message payload.
func ParseBindMessage(payload []byte) (portal string, stmtName string) {
	if len(payload) == 0 {
		return "", ""
	}
	pos := 0
	portal, pos = ReadCString(payload, pos)
	if pos < len(payload) {
		stmtName, _ = ReadCString(payload, pos)
	}
	return portal, stmtName
}

// ParseCommandComplete extracts the command tag from a CommandComplete ('C') message payload.
func ParseCommandComplete(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	tag, _ := ReadCString(payload, 0)
	return tag
}

// ParseErrorResponse extracts key fields from an ErrorResponse ('E') message payload.
// Returns severity, code (SQLSTATE), and message.
func ParseErrorResponse(payload []byte) (severity, code, message string) {
	pos := 0
	for pos < len(payload) {
		if payload[pos] == 0 {
			break // terminator
		}
		fieldType := payload[pos]
		pos++
		value, newPos := ReadCString(payload, pos)
		pos = newPos

		switch fieldType {
		case 'S': // Severity
			severity = value
		case 'C': // Code (SQLSTATE)
			code = value
		case 'M': // Message
			message = value
		}
	}
	return severity, code, message
}

// ParseStartupPayload extracts version and parameters from a StartupMessage payload.
// Payload starts after the 4-byte length (which was already consumed).
// First 4 bytes of payload are the version number.
func ParseStartupPayload(payload []byte) (version uint32, params map[string]string) {
	params = make(map[string]string)
	if len(payload) < 4 {
		return 0, params
	}

	version = binary.BigEndian.Uint32(payload[0:4])
	pos := 4

	// Parse key=value pairs (null-terminated strings, terminated by empty string)
	for pos < len(payload) {
		if payload[pos] == 0 {
			break
		}
		key, newPos := ReadCString(payload, pos)
		pos = newPos
		if pos >= len(payload) {
			break
		}
		value, newPos2 := ReadCString(payload, pos)
		pos = newPos2
		params[key] = value
	}

	return version, params
}

// IsSSLRequest checks if an untyped message payload represents an SSLRequest.
// The payload (after the 4-byte length) should be the 4-byte code.
func IsSSLRequest(payload []byte) bool {
	if len(payload) != 4 {
		return false
	}
	code := binary.BigEndian.Uint32(payload)
	return code == SSLRequestCode
}

// IsCancelRequest checks if an untyped message payload represents a CancelRequest.
func IsCancelRequest(payload []byte) bool {
	if len(payload) < 4 {
		return false
	}
	code := binary.BigEndian.Uint32(payload[:4])
	return code == CancelRequestCode
}
