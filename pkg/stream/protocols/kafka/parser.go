package kafka

import (
	"encoding/binary"
	"errors"
)

var (
	// ErrIncomplete indicates more data is needed to parse a complete message
	ErrIncomplete = errors.New("incomplete data")
	// ErrInvalidFormat indicates the data is not valid Kafka format
	ErrInvalidFormat = errors.New("invalid Kafka format")
)

// Parser handles Kafka wire protocol parsing
type Parser struct {
	buffer []byte
}

// NewParser creates a new Kafka parser
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

// ParseRequest attempts to parse the next complete Kafka request from the buffer.
// Returns ErrIncomplete if more data is needed.
func (p *Parser) ParseRequest() (*Request, error) {
	if len(p.buffer) < 4 {
		return nil, ErrIncomplete
	}

	// Read message size (4 bytes, big-endian)
	messageSize := int32(binary.BigEndian.Uint32(p.buffer[:4]))
	if messageSize < 8 || messageSize > 104857600 { // min 8 bytes for header, max 100MB
		// Skip this byte and try to resync
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}

	totalSize := int(messageSize) + 4 // message + length prefix
	if len(p.buffer) < totalSize {
		return nil, ErrIncomplete
	}

	// We have a complete message, parse the header
	data := p.buffer[4:totalSize]

	if len(data) < 8 {
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}

	apiKey := int16(binary.BigEndian.Uint16(data[0:2]))
	apiVersion := int16(binary.BigEndian.Uint16(data[2:4]))
	correlationID := int32(binary.BigEndian.Uint32(data[4:8]))

	// Validate ranges
	if apiKey < 0 || apiKey > 67 {
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}
	if apiVersion < 0 || apiVersion > 16 {
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}

	req := &Request{
		Header: RequestHeader{
			ApiKey:        apiKey,
			ApiVersion:    apiVersion,
			CorrelationID: correlationID,
		},
		MessageSize: messageSize,
		TotalSize:   totalSize,
	}

	// Parse client ID (after correlation_id)
	offset := 8
	if offset+2 <= len(data) {
		clientIDLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if clientIDLen > 0 && offset+int(clientIDLen) <= len(data) {
			req.Header.ClientID = string(data[offset : offset+int(clientIDLen)])
			offset += int(clientIDLen)
		} else if clientIDLen == -1 {
			// Null string - no client ID
		}
	}

	// Extract topic names and group IDs from specific API requests
	p.extractRequestDetails(req, data, offset, apiKey, apiVersion)

	// Consume the message from the buffer
	p.buffer = p.buffer[totalSize:]
	return req, nil
}

// ParseResponse attempts to parse the next complete Kafka response from the buffer.
// Returns ErrIncomplete if more data is needed.
func (p *Parser) ParseResponse() (*Response, error) {
	if len(p.buffer) < 4 {
		return nil, ErrIncomplete
	}

	// Read message size (4 bytes, big-endian)
	messageSize := int32(binary.BigEndian.Uint32(p.buffer[:4]))
	if messageSize < 4 || messageSize > 104857600 { // min 4 bytes for correlation_id, max 100MB
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}

	totalSize := int(messageSize) + 4
	if len(p.buffer) < totalSize {
		return nil, ErrIncomplete
	}

	data := p.buffer[4:totalSize]

	if len(data) < 4 {
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}

	correlationID := int32(binary.BigEndian.Uint32(data[0:4]))

	resp := &Response{
		Header: ResponseHeader{
			CorrelationID: correlationID,
		},
		MessageSize: messageSize,
		TotalSize:   totalSize,
	}

	// Try to extract error code from the response body
	// The error code position varies by API, but for many responses
	// it's at offset 4 (after correlation_id) as a top-level field
	if len(data) >= 6 {
		resp.ErrorCode = int16(binary.BigEndian.Uint16(data[4:6]))
	}

	// Consume the message from the buffer
	p.buffer = p.buffer[totalSize:]
	return resp, nil
}

// extractRequestDetails extracts topic names, group IDs, etc. from specific request types
func (p *Parser) extractRequestDetails(req *Request, data []byte, offset int, apiKey, apiVersion int16) {
	switch apiKey {
	case ApiKeyProduce:
		req.Topics = p.extractProduceTopics(data, offset, apiVersion)
	case ApiKeyFetch:
		req.Topics = p.extractFetchTopics(data, offset, apiVersion)
	case ApiKeyMetadata:
		req.Topics = p.extractMetadataTopics(data, offset, apiVersion)
	case ApiKeyJoinGroup:
		req.GroupID = p.extractGroupID(data, offset)
	case ApiKeySyncGroup:
		req.GroupID = p.extractGroupID(data, offset)
	case ApiKeyOffsetCommit:
		req.GroupID = p.extractGroupID(data, offset)
	case ApiKeyOffsetFetch:
		req.GroupID = p.extractGroupID(data, offset)
	case ApiKeyHeartbeat:
		req.GroupID = p.extractGroupID(data, offset)
	case ApiKeyLeaveGroup:
		req.GroupID = p.extractGroupID(data, offset)
	}
}

// extractProduceTopics extracts topic names from a Produce request
// Produce v0-v8: [transactional_id] acks timeout [topics]
func (p *Parser) extractProduceTopics(data []byte, offset int, apiVersion int16) []string {
	// For v3+, skip transactional_id (nullable string)
	if apiVersion >= 3 {
		offset = p.skipNullableString(data, offset)
		if offset < 0 {
			return nil
		}
	}

	// Skip acks (2 bytes) + timeout (4 bytes)
	offset += 6
	if offset+4 > len(data) {
		return nil
	}

	return p.extractTopicArray(data, offset)
}

// extractFetchTopics extracts topic names from a Fetch request
// Fetch v0+: replica_id max_wait_time min_bytes [max_bytes] [isolation_level] [session_id session_epoch] [topics]
func (p *Parser) extractFetchTopics(data []byte, offset int, apiVersion int16) []string {
	// Skip replica_id (4) + max_wait_time (4) + min_bytes (4) = 12 bytes
	offset += 12

	// v3+: max_bytes (4 bytes)
	if apiVersion >= 3 {
		offset += 4
	}
	// v4+: isolation_level (1 byte)
	if apiVersion >= 4 {
		offset += 1
	}
	// v7+: session_id (4 bytes) + session_epoch (4 bytes)
	if apiVersion >= 7 {
		offset += 8
	}

	if offset+4 > len(data) {
		return nil
	}

	return p.extractTopicArray(data, offset)
}

// extractMetadataTopics extracts topic names from a Metadata request
// Metadata v0+: [topics]
func (p *Parser) extractMetadataTopics(data []byte, offset int, apiVersion int16) []string {
	if offset+4 > len(data) {
		return nil
	}
	return p.extractTopicArray(data, offset)
}

// extractTopicArray reads a Kafka array of topic structures where first field is the topic name
func (p *Parser) extractTopicArray(data []byte, offset int) []string {
	if offset+4 > len(data) {
		return nil
	}

	arrayLen := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	if arrayLen <= 0 || arrayLen > 10000 {
		return nil
	}

	topics := make([]string, 0, arrayLen)
	for i := int32(0); i < arrayLen; i++ {
		if offset+2 > len(data) {
			break
		}
		topicLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if topicLen <= 0 || offset+int(topicLen) > len(data) {
			break
		}
		topics = append(topics, string(data[offset:offset+int(topicLen)]))
		offset += int(topicLen)

		// We can't easily skip the rest of the topic structure without
		// knowing the exact schema version, so just collect the first few topics
		// and break if we can't parse further
		break // Only extract the first topic for simplicity
	}

	return topics
}

// extractGroupID reads a string (group_id) at the given offset
func (p *Parser) extractGroupID(data []byte, offset int) string {
	if offset+2 > len(data) {
		return ""
	}
	strLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if strLen <= 0 || offset+int(strLen) > len(data) {
		return ""
	}
	return string(data[offset : offset+int(strLen)])
}

// skipNullableString skips a nullable string (int16 length + data, -1 for null)
func (p *Parser) skipNullableString(data []byte, offset int) int {
	if offset+2 > len(data) {
		return -1
	}
	strLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if strLen == -1 {
		return offset // null string
	}
	if strLen < 0 || offset+int(strLen) > len(data) {
		return -1
	}
	return offset + int(strLen)
}
