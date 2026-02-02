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
func ParseCommand(pkt *Packet) (byte, interface{}, error) {
	if len(pkt.Payload) < 1 {
		return 0, nil, ErrInvalidPacket
	}

	cmd := pkt.Payload[0]

	switch cmd {
	case ComQuery:
		return cmd, &QueryCommand{
			Query: string(pkt.Payload[1:]),
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
func ParseResponse(pkt *Packet) (interface{}, error) {
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
	authData := make([]byte, 8)
	copy(authData, pkt.Payload[pos:pos+8])
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

// readLengthEncodedString reads a length-encoded string
func readLengthEncodedString(data []byte) (string, int, error) {
	length, n, err := readLengthEncodedInt(data)
	if err != nil {
		return "", 0, err
	}

	if len(data) < n+int(length) {
		return "", 0, ErrIncomplete
	}

	return string(data[n : n+int(length)]), n + int(length), nil
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

// String returns a human-readable representation of the packet
func (p *Packet) String() string {
	return fmt.Sprintf("Packet{len=%d, seq=%d}", p.Length, p.SequenceID)
}
