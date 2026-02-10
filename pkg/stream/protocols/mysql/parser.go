package mysql

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	// ErrIncomplete indicates more data is needed
	ErrIncomplete = errors.New("incomplete packet")
	// ErrInvalidPacket indicates the packet format is invalid
	ErrInvalidPacket = errors.New("invalid packet format")
	// ErrBufferFull indicates the buffer exceeded the maximum size
	ErrBufferFull = errors.New("buffer exceeded maximum size")
)

const (
	// MaxBufferSize is the maximum parser buffer size: MySQL max packet (16MB) + 4-byte header.
	MaxBufferSize = 16*1024*1024 + 4
)

// Parser handles MySQL protocol parsing
type Parser struct {
	buffer []byte
}

// NewParser creates a new MySQL protocol parser
func NewParser() *Parser {
	return &Parser{
		buffer: make([]byte, 0, 4096),
	}
}

// Append adds data to the parser buffer.
// Returns ErrBufferFull if the buffer would exceed MaxBufferSize.
func (p *Parser) Append(data []byte) error {
	if len(p.buffer)+len(data) > MaxBufferSize {
		p.buffer = p.buffer[:0]
		return ErrBufferFull
	}
	p.buffer = append(p.buffer, data...)
	return nil
}

// BufferLen returns the current buffer length
func (p *Parser) BufferLen() int {
	return len(p.buffer)
}

// Reset clears the parser buffer
func (p *Parser) Reset() {
	p.buffer = p.buffer[:0]
}

// ParsePacket attempts to parse the next complete MySQL packet
// Returns ErrIncomplete if more data is needed
func (p *Parser) ParsePacket() (*Packet, error) {
	if len(p.buffer) < 4 {
		return nil, ErrIncomplete
	}

	// MySQL packet header: 3 bytes length + 1 byte sequence ID
	length := uint32(p.buffer[0]) | uint32(p.buffer[1])<<8 | uint32(p.buffer[2])<<16
	seqID := p.buffer[3]

	// Check if we have the full payload
	totalLen := 4 + int(length)
	if len(p.buffer) < totalLen {
		return nil, ErrIncomplete
	}

	pkt := &Packet{
		Length:     length,
		SequenceID: seqID,
		Payload:    make([]byte, length),
	}
	copy(pkt.Payload, p.buffer[4:totalLen])

	// Remove consumed bytes
	p.buffer = p.buffer[totalLen:]

	return pkt, nil
}

// ParseCommand parses a command packet from the client
func ParseCommand(pkt *Packet) (byte, any, error) {
	if len(pkt.Payload) < 1 {
		return 0, nil, ErrInvalidPacket
	}

	cmd := pkt.Payload[0]

	switch cmd {
	case ComQuery:
		query := pkt.Payload[1:]
		// MySQL 8.0+ clients with CLIENT_QUERY_ATTRIBUTES prepend a
		// parameter-count (lenenc int) and parameter-set-count (lenenc int, always 1)
		// before the SQL text.  When no attributes are sent the prefix is
		// [0x00, 0x01].  Detect and skip this so the query text is clean.
		// Valid SQL never starts with a NUL byte, so this is safe.
		if len(query) >= 2 && query[0] == 0x00 && query[1] == 0x01 {
			query = query[2:]
		}
		return cmd, &QueryCommand{
			Query: string(query),
		}, nil
	case ComQuit:
		return cmd, nil, nil
	case ComPing:
		return cmd, nil, nil
	case ComInitDB:
		return cmd, string(pkt.Payload[1:]), nil
	case ComStmtPrepare:
		return cmd, &QueryCommand{
			Query: string(pkt.Payload[1:]),
		}, nil
	default:
		return cmd, pkt.Payload[1:], nil
	}
}

// ParseResponse parses a response packet from the server
func ParseResponse(pkt *Packet) (any, error) {
	if len(pkt.Payload) < 1 {
		return nil, ErrInvalidPacket
	}

	switch pkt.Payload[0] {
	case OKPacket:
		return parseOKPacket(pkt.Payload)
	case ERRPacket:
		return parseERRPacket(pkt.Payload)
	case EOFPacket:
		if len(pkt.Payload) < 9 {
			// EOF packet (legacy)
			return "EOF", nil
		}
		// Could be OK packet in newer protocol
		return parseOKPacket(pkt.Payload)
	default:
		// Could be a result set (column count) or local infile
		if pkt.Payload[0] == LocalInfile {
			return "LOCAL_INFILE", nil
		}
		// First byte is column count (length-encoded int)
		colCount, _, err := readLengthEncodedInt(pkt.Payload)
		if err != nil {
			return nil, err
		}
		return &ResultSet{ColumnCount: colCount}, nil
	}
}

func parseOKPacket(data []byte) (*OKResponse, error) {
	if len(data) < 1 || (data[0] != OKPacket && data[0] != EOFPacket) {
		return nil, ErrInvalidPacket
	}

	pos := 1
	ok := &OKResponse{}

	// Affected rows
	var n int
	ok.AffectedRows, n, _ = readLengthEncodedInt(data[pos:])
	pos += n

	// Last insert ID
	ok.LastInsertID, n, _ = readLengthEncodedInt(data[pos:])
	pos += n

	// Status flags (2 bytes)
	if pos+2 <= len(data) {
		ok.StatusFlags = binary.LittleEndian.Uint16(data[pos:])
		pos += 2
	}

	// Warnings (2 bytes)
	if pos+2 <= len(data) {
		ok.Warnings = binary.LittleEndian.Uint16(data[pos:])
		pos += 2
	}

	// Info (rest of packet)
	if pos < len(data) {
		ok.Info = string(data[pos:])
	}

	return ok, nil
}

func parseERRPacket(data []byte) (*ERRResponse, error) {
	if len(data) < 3 || data[0] != ERRPacket {
		return nil, ErrInvalidPacket
	}

	err := &ERRResponse{
		ErrorCode: binary.LittleEndian.Uint16(data[1:3]),
	}

	pos := 3

	// SQL state marker '#' and 5-byte state
	if pos < len(data) && data[pos] == '#' {
		pos++
		if pos+5 <= len(data) {
			err.SQLState = string(data[pos : pos+5])
			pos += 5
		}
	}

	// Error message
	if pos < len(data) {
		err.ErrorMessage = string(data[pos:])
	}

	return err, nil
}

// ParseServerHandshake parses the initial handshake packet from server
func ParseServerHandshake(pkt *Packet) (*ServerHandshake, error) {
	if len(pkt.Payload) < 5 {
		return nil, ErrInvalidPacket
	}

	hs := &ServerHandshake{
		ProtocolVersion: pkt.Payload[0],
	}

	// Server version (null-terminated string)
	pos := 1
	for i := pos; i < len(pkt.Payload); i++ {
		if pkt.Payload[i] == 0 {
			hs.ServerVersion = string(pkt.Payload[pos:i])
			pos = i + 1
			break
		}
	}

	// Connection ID (4 bytes)
	if pos+4 > len(pkt.Payload) {
		return hs, nil
	}
	hs.ConnectionID = binary.LittleEndian.Uint32(pkt.Payload[pos:])
	pos += 4

	// Auth plugin data part 1 (8 bytes)
	if pos+8 > len(pkt.Payload) {
		return hs, nil
	}
	authData := make([]byte, 0, 21) // 8 + 13 max
	authData = append(authData, pkt.Payload[pos:pos+8]...)
	pos += 8

	// Filler (1 byte)
	pos++

	// Capability flags lower 2 bytes
	if pos+2 > len(pkt.Payload) {
		return hs, nil
	}
	capLower := binary.LittleEndian.Uint16(pkt.Payload[pos:])
	pos += 2

	// Character set (1 byte)
	if pos >= len(pkt.Payload) {
		hs.CapabilityFlags = uint32(capLower)
		hs.AuthPluginData = authData
		return hs, nil
	}
	hs.CharacterSet = pkt.Payload[pos]
	pos++

	// Status flags (2 bytes)
	if pos+2 > len(pkt.Payload) {
		return hs, nil
	}
	hs.StatusFlags = binary.LittleEndian.Uint16(pkt.Payload[pos:])
	pos += 2

	// Capability flags upper 2 bytes
	if pos+2 > len(pkt.Payload) {
		hs.CapabilityFlags = uint32(capLower)
		hs.AuthPluginData = authData
		return hs, nil
	}
	capUpper := binary.LittleEndian.Uint16(pkt.Payload[pos:])
	pos += 2
	hs.CapabilityFlags = uint32(capLower) | uint32(capUpper)<<16

	// Auth plugin data length (1 byte)
	if pos >= len(pkt.Payload) {
		hs.AuthPluginData = authData
		return hs, nil
	}
	authLen := int(pkt.Payload[pos])
	pos++

	// Reserved (10 bytes)
	pos += 10

	// Auth plugin data part 2 (if CLIENT_SECURE_CONNECTION)
	if pos < len(pkt.Payload) && authLen > 8 {
		part2Len := authLen - 8
		if part2Len > 13 {
			part2Len = 13
		}
		if pos+part2Len <= len(pkt.Payload) {
			authData = append(authData, pkt.Payload[pos:pos+part2Len]...)
			pos += part2Len
		}
	}
	hs.AuthPluginData = authData

	// Auth plugin name (null-terminated)
	if pos < len(pkt.Payload) {
		for i := pos; i < len(pkt.Payload); i++ {
			if pkt.Payload[i] == 0 {
				hs.AuthPluginName = string(pkt.Payload[pos:i])
				break
			}
		}
	}

	return hs, nil
}

// readLengthEncodedInt reads a length-encoded integer
func readLengthEncodedInt(data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, ErrIncomplete
	}

	switch data[0] {
	case 0xfb: // NULL
		return 0, 1, nil
	case 0xfc: // 2-byte int
		if len(data) < 3 {
			return 0, 0, ErrIncomplete
		}
		return uint64(binary.LittleEndian.Uint16(data[1:3])), 3, nil
	case 0xfd: // 3-byte int
		if len(data) < 4 {
			return 0, 0, ErrIncomplete
		}
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16, 4, nil
	case 0xfe: // 8-byte int
		if len(data) < 9 {
			return 0, 0, ErrIncomplete
		}
		return binary.LittleEndian.Uint64(data[1:9]), 9, nil
	default:
		return uint64(data[0]), 1, nil
	}
}

// BuildQueryPacket creates a COM_QUERY packet
func BuildQueryPacket(seqID byte, query string) []byte {
	payload := append([]byte{ComQuery}, []byte(query)...)
	return buildPacket(seqID, payload)
}

// BuildPingPacket creates a COM_PING packet
func BuildPingPacket(seqID byte) []byte {
	return buildPacket(seqID, []byte{ComPing})
}

// BuildQuitPacket creates a COM_QUIT packet
func BuildQuitPacket(seqID byte) []byte {
	return buildPacket(seqID, []byte{ComQuit})
}

func buildPacket(seqID byte, payload []byte) []byte {
	length := len(payload)
	pkt := make([]byte, 4+length)
	pkt[0] = byte(length)
	pkt[1] = byte(length >> 8)
	pkt[2] = byte(length >> 16)
	pkt[3] = seqID
	copy(pkt[4:], payload)
	return pkt
}

// Result set size limits
const (
	MaxResultSetRows = 100
	MaxValueSize     = 64 * 1024 // 64KB
)

// readLengthEncodedString reads a length-encoded string from data.
// Returns the string, bytes consumed, and any error.
func readLengthEncodedString(data []byte) (string, int, error) {
	if len(data) == 0 {
		return "", 0, ErrIncomplete
	}

	// NULL indicator
	if data[0] == 0xfb {
		return "", 1, nil
	}

	length, n, err := readLengthEncodedInt(data)
	if err != nil {
		return "", 0, err
	}

	if uint64(len(data)-n) < length {
		return "", 0, ErrIncomplete
	}

	return string(data[n : n+int(length)]), n + int(length), nil
}

// parseColumnDefinition parses a column definition packet (COM_QUERY response).
func parseColumnDefinition(data []byte) (*ColumnDefinition, error) {
	col := &ColumnDefinition{}
	pos := 0

	// catalog (length-encoded string)
	s, n, err := readLengthEncodedString(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading catalog: %w", err)
	}
	col.Catalog = s
	pos += n

	// schema
	s, n, err = readLengthEncodedString(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading schema: %w", err)
	}
	col.Schema = s
	pos += n

	// table (virtual)
	s, n, err = readLengthEncodedString(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading table: %w", err)
	}
	col.Table = s
	pos += n

	// org_table (physical)
	s, n, err = readLengthEncodedString(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading org_table: %w", err)
	}
	col.OrgTable = s
	pos += n

	// name (virtual)
	s, n, err = readLengthEncodedString(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading name: %w", err)
	}
	col.Name = s
	pos += n

	// org_name (physical)
	s, n, err = readLengthEncodedString(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading org_name: %w", err)
	}
	col.OrgName = s
	pos += n

	// fixed-length fields marker (0x0c)
	if pos >= len(data) {
		return nil, ErrIncomplete
	}
	pos++ // skip 0x0c length marker

	// Need at least 12 more bytes for fixed fields
	if pos+12 > len(data) {
		return nil, ErrIncomplete
	}

	col.CharacterSet = binary.LittleEndian.Uint16(data[pos:])
	pos += 2
	col.ColumnLength = binary.LittleEndian.Uint32(data[pos:])
	pos += 4
	col.ColumnType = data[pos]
	pos++
	col.Flags = binary.LittleEndian.Uint16(data[pos:])
	pos += 2
	col.Decimals = data[pos]
	// pos++ + 2 filler bytes — we don't need them

	return col, nil
}

// parseResultSetRow parses a text protocol result set row.
// Each field is a length-encoded string, or 0xfb for NULL.
func parseResultSetRow(data []byte, columnCount int) ([]Value, error) {
	values := make([]Value, 0, columnCount)
	pos := 0

	for i := range columnCount {
		if pos >= len(data) {
			return nil, fmt.Errorf("unexpected end of row data at column %d", i)
		}

		if data[pos] == 0xfb {
			// NULL
			values = append(values, Value{IsNull: true})
			pos++
			continue
		}

		// Length-encoded string
		length, n, err := readLengthEncodedInt(data[pos:])
		if err != nil {
			return nil, fmt.Errorf("reading row value length at column %d: %w", i, err)
		}
		pos += n

		if uint64(len(data)-pos) < length {
			return nil, fmt.Errorf("incomplete row value at column %d", i)
		}

		valData := data[pos : pos+int(length)]
		// Truncate large values
		if len(valData) > MaxValueSize {
			marker := []byte("...[truncated]")
			buf := make([]byte, 0, MaxValueSize+len(marker))
			buf = append(buf, valData[:MaxValueSize]...)
			buf = append(buf, marker...)
			valData = buf
		} else {
			cp := make([]byte, len(valData))
			copy(cp, valData)
			valData = cp
		}

		values = append(values, Value{Data: valData})
		pos += int(length)
	}

	return values, nil
}

// IsError checks if a packet is an error response
func IsError(pkt *Packet) bool {
	return len(pkt.Payload) > 0 && pkt.Payload[0] == ERRPacket
}

// IsOK checks if a packet is an OK response
func IsOK(pkt *Packet) bool {
	return len(pkt.Payload) > 0 && pkt.Payload[0] == OKPacket
}

// IsEOF checks if a packet is an EOF packet
func IsEOF(pkt *Packet) bool {
	return len(pkt.Payload) > 0 && pkt.Payload[0] == EOFPacket && len(pkt.Payload) < 9
}

// isOKTerminator checks if a packet is an OK terminator used in
// CLIENT_DEPRECATE_EOF mode. In this mode the server sends a packet
// with 0xFE header and length >= 9 (an OK packet in EOF's clothing).
func isOKTerminator(pkt *Packet) bool {
	return len(pkt.Payload) >= 9 && pkt.Payload[0] == EOFPacket
}

// String returns a human-readable representation of the packet
func (p *Packet) String() string {
	return fmt.Sprintf("Packet{len=%d, seq=%d}", p.Length, p.SequenceID)
}
