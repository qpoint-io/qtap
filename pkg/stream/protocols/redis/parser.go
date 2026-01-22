package redis

import (
	"bytes"
	"errors"
	"math"
	"strconv"
)

var (
	// ErrIncomplete indicates more data is needed to parse a complete value
	ErrIncomplete = errors.New("incomplete data")
	// ErrInvalidFormat indicates the data is not valid RESP format
	ErrInvalidFormat = errors.New("invalid RESP format")
)

// crlf is the RESP line terminator
var crlf = []byte("\r\n")

// Parser handles RESP protocol parsing
type Parser struct {
	buffer []byte
}

// NewParser creates a new RESP parser
func NewParser() *Parser {
	return &Parser{
		buffer: make([]byte, 0),
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

// Parse attempts to parse and return the next complete RESP value.
// Returns ErrIncomplete if more data is needed.
func (p *Parser) Parse() (*Value, error) {
	if len(p.buffer) == 0 {
		return nil, ErrIncomplete
	}

	value, consumed, err := p.parseValue(p.buffer)
	if err != nil {
		return nil, err
	}

	value.size = consumed

	// Remove consumed bytes from buffer
	p.buffer = p.buffer[consumed:]
	return value, nil
}

// parseValue parses a RESP value and returns bytes consumed
func (p *Parser) parseValue(data []byte) (*Value, int, error) {
	if len(data) == 0 {
		return nil, 0, ErrIncomplete
	}

	switch RESPType(data[0]) {
	case TypeSimpleString:
		return p.parseSimpleString(data)
	case TypeError:
		return p.parseError(data)
	case TypeInteger:
		return p.parseInteger(data)
	case TypeBulkString:
		return p.parseBulkString(data)
	case TypeArray:
		return p.parseArray(data)
	case TypeNull:
		return p.parseNull(data)
	case TypeBoolean:
		return p.parseBoolean(data)
	case TypeDouble:
		return p.parseDouble(data)
	case TypeBigNumber:
		return p.parseBigNumber(data)
	case TypeBulkError:
		return p.parseBulkError(data)
	case TypeVerbatim:
		return p.parseVerbatim(data)
	case TypeMap:
		return p.parseMap(data)
	case TypeSet:
		return p.parseSet(data)
	case TypePush:
		return p.parsePush(data)
	default:
		return nil, 0, ErrInvalidFormat
	}
}

// parseSimpleString parses a simple string: +<string>\r\n
func (p *Parser) parseSimpleString(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	return &Value{
		Type: TypeSimpleString,
		Str:  string(data[1:idx]),
	}, idx + 2, nil
}

// parseError parses an error: -<string>\r\n
func (p *Parser) parseError(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	return &Value{
		Type: TypeError,
		Str:  string(data[1:idx]),
	}, idx + 2, nil
}

// parseInteger parses an integer: :[<+|->]<value>\r\n
func (p *Parser) parseInteger(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	val, err := strconv.ParseInt(string(data[1:idx]), 10, 64)
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	return &Value{
		Type: TypeInteger,
		Int:  val,
	}, idx + 2, nil
}

// parseBulkString parses a bulk string: $<length>\r\n<data>\r\n
func (p *Parser) parseBulkString(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	length, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	// Null bulk string (RESP2)
	if length < 0 {
		return &Value{
			Type:   TypeBulkString,
			IsNull: true,
		}, idx + 2, nil
	}

	// Check if we have the full string + trailing CRLF
	start := idx + 2
	end := start + length
	if end+2 > len(data) {
		return nil, 0, ErrIncomplete
	}

	return &Value{
		Type: TypeBulkString,
		Str:  string(data[start:end]),
	}, end + 2, nil
}

// parseArray parses an array: *<count>\r\n<elements>
func (p *Parser) parseArray(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	count, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	// Null array (RESP2)
	if count < 0 {
		return &Value{
			Type:   TypeArray,
			IsNull: true,
		}, idx + 2, nil
	}

	pos := idx + 2
	elements := make([]Value, 0, count)

	for range count {
		if pos >= len(data) {
			return nil, 0, ErrIncomplete
		}

		elem, consumed, err := p.parseValue(data[pos:])
		if err != nil {
			return nil, 0, err
		}

		elements = append(elements, *elem)
		pos += consumed
	}

	return &Value{
		Type:  TypeArray,
		Array: elements,
	}, pos, nil
}

// parseNull parses a null: _\r\n (RESP3)
func (p *Parser) parseNull(data []byte) (*Value, int, error) {
	if len(data) < 3 {
		return nil, 0, ErrIncomplete
	}
	if data[1] != '\r' || data[2] != '\n' {
		return nil, 0, ErrInvalidFormat
	}

	return &Value{
		Type:   TypeNull,
		IsNull: true,
	}, 3, nil
}

// parseBoolean parses a boolean: #<t|f>\r\n (RESP3)
func (p *Parser) parseBoolean(data []byte) (*Value, int, error) {
	if len(data) < 4 {
		return nil, 0, ErrIncomplete
	}
	if data[2] != '\r' || data[3] != '\n' {
		return nil, 0, ErrInvalidFormat
	}

	var val bool
	switch data[1] {
	case 't':
		val = true
	case 'f':
		val = false
	default:
		return nil, 0, ErrInvalidFormat
	}

	return &Value{
		Type: TypeBoolean,
		Bool: val,
	}, 4, nil
}

// parseDouble parses a double: ,<value>\r\n (RESP3)
func (p *Parser) parseDouble(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	str := string(data[1:idx])

	// Handle special values
	var val float64
	var err error
	switch str {
	case "inf":
		val = positiveInf()
	case "-inf":
		val = negativeInf()
	case "nan":
		val = nan()
	default:
		val, err = strconv.ParseFloat(str, 64)
		if err != nil {
			return nil, 0, ErrInvalidFormat
		}
	}

	return &Value{
		Type:  TypeDouble,
		Float: val,
	}, idx + 2, nil
}

// parseBigNumber parses a big number: (<number>\r\n (RESP3)
func (p *Parser) parseBigNumber(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	// Store as string since it may exceed int64
	return &Value{
		Type: TypeBigNumber,
		Str:  string(data[1:idx]),
	}, idx + 2, nil
}

// parseBulkError parses a bulk error: !<length>\r\n<error>\r\n (RESP3)
func (p *Parser) parseBulkError(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	length, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	start := idx + 2
	end := start + length
	if end+2 > len(data) {
		return nil, 0, ErrIncomplete
	}

	return &Value{
		Type: TypeBulkError,
		Str:  string(data[start:end]),
	}, end + 2, nil
}

// parseVerbatim parses a verbatim string: =<length>\r\n<encoding>:<data>\r\n (RESP3)
func (p *Parser) parseVerbatim(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	length, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	start := idx + 2
	end := start + length
	if end+2 > len(data) {
		return nil, 0, ErrIncomplete
	}

	// Verbatim format: <3-byte-encoding>:<data>
	// We store the full content including encoding prefix
	return &Value{
		Type: TypeVerbatim,
		Str:  string(data[start:end]),
	}, end + 2, nil
}

// parseMap parses a map: %<count>\r\n<key1><value1>... (RESP3)
func (p *Parser) parseMap(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	count, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	pos := idx + 2
	m := make(map[string]Value, count)

	for range count {
		// Parse key
		if pos >= len(data) {
			return nil, 0, ErrIncomplete
		}
		key, keyConsumed, err := p.parseValue(data[pos:])
		if err != nil {
			return nil, 0, err
		}
		pos += keyConsumed

		// Parse value
		if pos >= len(data) {
			return nil, 0, ErrIncomplete
		}
		val, valConsumed, err := p.parseValue(data[pos:])
		if err != nil {
			return nil, 0, err
		}
		pos += valConsumed

		// Use string representation of key
		m[key.Str] = *val
	}

	return &Value{
		Type: TypeMap,
		Map:  m,
	}, pos, nil
}

// parseSet parses a set: ~<count>\r\n<elements> (RESP3)
func (p *Parser) parseSet(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	count, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	pos := idx + 2
	elements := make([]Value, 0, count)

	for range count {
		if pos >= len(data) {
			return nil, 0, ErrIncomplete
		}

		elem, consumed, err := p.parseValue(data[pos:])
		if err != nil {
			return nil, 0, err
		}

		elements = append(elements, *elem)
		pos += consumed
	}

	return &Value{
		Type:  TypeSet,
		Array: elements,
	}, pos, nil
}

// parsePush parses a push: ><count>\r\n<elements> (RESP3)
func (p *Parser) parsePush(data []byte) (*Value, int, error) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, 0, ErrIncomplete
	}

	count, err := strconv.Atoi(string(data[1:idx]))
	if err != nil {
		return nil, 0, ErrInvalidFormat
	}

	pos := idx + 2
	elements := make([]Value, 0, count)

	for range count {
		if pos >= len(data) {
			return nil, 0, ErrIncomplete
		}

		elem, consumed, err := p.parseValue(data[pos:])
		if err != nil {
			return nil, 0, err
		}

		elements = append(elements, *elem)
		pos += consumed
	}

	return &Value{
		Type:  TypePush,
		Array: elements,
	}, pos, nil
}

// Helper functions for special float values
func positiveInf() float64 {
	return math.Inf(1)
}

func negativeInf() float64 {
	return math.Inf(-1)
}

func nan() float64 {
	return math.NaN()
}
