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

// isFlexibleVersion returns true if the given API key and version use the
// flexible wire format (KIP-482). In flexible versions, the request header
// uses header v2 (with tagged fields after clientId), and the body uses
// COMPACT_STRING and COMPACT_ARRAY encodings.
func isFlexibleVersion(apiKey, apiVersion int16) bool {
	switch apiKey {
	case ApiKeyProduce:
		return apiVersion >= 9
	case ApiKeyFetch:
		return apiVersion >= 12
	case ApiKeyListOffsets:
		return apiVersion >= 6
	case ApiKeyMetadata:
		return apiVersion >= 9
	case ApiKeyOffsetCommit:
		return apiVersion >= 8
	case ApiKeyOffsetFetch:
		return apiVersion >= 6
	case ApiKeyFindCoordinator:
		return apiVersion >= 3
	case ApiKeyJoinGroup:
		return apiVersion >= 6
	case ApiKeyHeartbeat:
		return apiVersion >= 4
	case ApiKeyLeaveGroup:
		return apiVersion >= 4
	case ApiKeySyncGroup:
		return apiVersion >= 4
	case ApiKeyDescribeGroups:
		return apiVersion >= 5
	case ApiKeyListGroups:
		return apiVersion >= 3
	case ApiKeySaslHandshake:
		return apiVersion >= 1
	case ApiKeyApiVersions:
		// ApiVersions is special: flexible at v3+ but header stays v1 (no tagged fields)
		// to maintain backward compatibility. We return false here so the header
		// parsing doesn't try to skip tagged fields, but note that if we ever
		// need to parse ApiVersions request bodies, they'd use compact encoding at v3+.
		return false
	case ApiKeyCreateTopics:
		return apiVersion >= 5
	case ApiKeyDeleteTopics:
		return apiVersion >= 4
	case ApiKeySaslAuthenticate:
		return apiVersion >= 2
	default:
		// For APIs we don't specifically track, assume non-flexible.
		// This is safe because we only use this for topic/group extraction
		// on the APIs listed above.
		return false
	}
}

// readUnsignedVarint reads an unsigned variable-length integer (as used in Kafka's
// compact encoding). Returns the value and the number of bytes consumed, or -1
// if the data is insufficient.
func readUnsignedVarint(data []byte, offset int) (uint32, int) {
	var result uint32
	shift := uint(0)
	for i := 0; i < 5; i++ { // max 5 bytes for uint32
		if offset+i >= len(data) {
			return 0, -1
		}
		b := data[offset+i]
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1
		}
		shift += 7
	}
	return 0, -1 // overflow
}

// skipTaggedFields skips the tagged fields section in a flexible-version message.
// Tagged fields are encoded as: unsigned varint numTags, then for each tag:
// unsigned varint tagType, unsigned varint dataLen, dataLen bytes of data.
// Returns the new offset, or -1 on error.
func skipTaggedFields(data []byte, offset int) int {
	numTags, n := readUnsignedVarint(data, offset)
	if n < 0 {
		return -1
	}
	offset += n
	for i := uint32(0); i < numTags; i++ {
		// tag type
		_, n = readUnsignedVarint(data, offset)
		if n < 0 {
			return -1
		}
		offset += n
		// data length
		dataLen, n := readUnsignedVarint(data, offset)
		if n < 0 {
			return -1
		}
		offset += n
		if offset+int(dataLen) > len(data) {
			return -1
		}
		offset += int(dataLen)
	}
	return offset
}

// readCompactString reads a COMPACT_STRING at the given offset.
// COMPACT_STRING: unsigned varint length+1 (0 = null, 1 = empty, N = N-1 bytes).
// Returns the string, new offset, or offset -1 on error.
func readCompactString(data []byte, offset int) (string, int) {
	rawLen, n := readUnsignedVarint(data, offset)
	if n < 0 {
		return "", -1
	}
	offset += n
	if rawLen == 0 {
		return "", offset // null string
	}
	strLen := int(rawLen - 1)
	if offset+strLen > len(data) {
		return "", -1
	}
	s := string(data[offset : offset+strLen])
	return s, offset + strLen
}

// readCompactNullableString reads a COMPACT_NULLABLE_STRING (same encoding as compact string).
func readCompactNullableString(data []byte, offset int) (string, int) {
	return readCompactString(data, offset)
}

// readCompactArrayLen reads the length of a COMPACT_ARRAY.
// COMPACT_ARRAY: unsigned varint count+1 (0 = null, N = N-1 elements).
// Returns the array length (or -1 for null) and new offset.
func readCompactArrayLen(data []byte, offset int) (int, int) {
	rawLen, n := readUnsignedVarint(data, offset)
	if n < 0 {
		return -1, -1
	}
	offset += n
	if rawLen == 0 {
		return -1, offset // null array
	}
	return int(rawLen - 1), offset
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
	if apiKey < 0 || apiKey > 74 {
		p.buffer = p.buffer[1:]
		return nil, ErrInvalidFormat
	}
	if apiVersion < 0 || apiVersion > 20 {
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

	flexible := isFlexibleVersion(apiKey, apiVersion)

	// Parse client ID (after correlation_id).
	// The ClientId field always uses old-style INT16 length encoding, even in
	// flexible versions (per KIP-482 / RequestHeader spec).
	offset := 8
	if offset+2 <= len(data) {
		clientIDLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if clientIDLen > 0 && offset+int(clientIDLen) <= len(data) {
			req.Header.ClientID = string(data[offset : offset+int(clientIDLen)])
			offset += int(clientIDLen)
		} else if clientIDLen == -1 {
			// Null client ID: no data bytes to read, offset already past length prefix
			_ = clientIDLen
		}
	}

	// For flexible versions, the header has tagged fields after the client ID
	if flexible {
		newOffset := skipTaggedFields(data, offset)
		if newOffset < 0 {
			// Can't parse tagged fields; skip topic extraction but still count it as valid
			p.buffer = p.buffer[totalSize:]
			return req, nil
		}
		offset = newOffset
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
	flexible := isFlexibleVersion(apiKey, apiVersion)

	switch apiKey {
	case ApiKeyProduce:
		req.Topics = p.extractProduceTopics(data, offset, apiVersion, flexible)
	case ApiKeyFetch:
		req.Topics = p.extractFetchTopics(data, offset, apiVersion, flexible)
	case ApiKeyMetadata:
		req.Topics = p.extractMetadataTopics(data, offset, apiVersion, flexible)
	case ApiKeyCreateTopics:
		req.Topics = p.extractCreateTopicsTopics(data, offset, apiVersion, flexible)
	case ApiKeyJoinGroup:
		req.GroupID = p.extractGroupID(data, offset, flexible)
	case ApiKeySyncGroup:
		req.GroupID = p.extractGroupID(data, offset, flexible)
	case ApiKeyOffsetCommit:
		req.GroupID = p.extractGroupID(data, offset, flexible)
	case ApiKeyOffsetFetch:
		req.GroupID = p.extractGroupID(data, offset, flexible)
	case ApiKeyHeartbeat:
		req.GroupID = p.extractGroupID(data, offset, flexible)
	case ApiKeyLeaveGroup:
		req.GroupID = p.extractGroupID(data, offset, flexible)
	}
}

// extractProduceTopics extracts topic names from a Produce request.
// Produce v3+: transactional_id (nullable string) + acks(2) + timeout(4) + [topic_data]
// Flexible at v9+.
func (p *Parser) extractProduceTopics(data []byte, offset int, apiVersion int16, flexible bool) []string {
	// Produce v3+: skip transactional_id (always present in v3+)
	if flexible {
		// COMPACT_NULLABLE_STRING
		_, newOffset := readCompactNullableString(data, offset)
		if newOffset < 0 {
			return nil
		}
		offset = newOffset
	} else if apiVersion >= 3 {
		// Old-style nullable string
		offset = p.skipNullableString(data, offset)
		if offset < 0 {
			return nil
		}
	}

	// Skip acks (2 bytes) + timeout (4 bytes)
	offset += 6
	if offset > len(data) {
		return nil
	}

	return p.extractTopicArray(data, offset, flexible)
}

// extractFetchTopics extracts topic names from a Fetch request.
// Fetch v4+: replica_id(4) + max_wait_time(4) + min_bytes(4) + [max_bytes(4)] + [isolation_level(1)] + [session_id(4)+session_epoch(4)] + [topics]
// Flexible at v12+.
func (p *Parser) extractFetchTopics(data []byte, offset int, apiVersion int16, flexible bool) []string {
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

	if offset > len(data) {
		return nil
	}

	return p.extractTopicArray(data, offset, flexible)
}

// extractMetadataTopics extracts topic names from a Metadata request.
// Metadata v0+: [topics]
// Flexible at v9+.
func (p *Parser) extractMetadataTopics(data []byte, offset int, apiVersion int16, flexible bool) []string {
	if offset > len(data) {
		return nil
	}
	return p.extractTopicArray(data, offset, flexible)
}

// extractCreateTopicsTopics extracts topic names from a CreateTopics request.
// CreateTopics: [topics] + timeout_ms(4) + [validate_only(1)]
// Flexible at v5+.
func (p *Parser) extractCreateTopicsTopics(data []byte, offset int, apiVersion int16, flexible bool) []string {
	if offset > len(data) {
		return nil
	}
	return p.extractTopicArray(data, offset, flexible)
}

// extractTopicArray reads a Kafka array of topic structures where the first field is the topic name.
// For non-flexible versions: INT32 array length, then INT16 string length + data for each topic.
// For flexible versions: COMPACT_ARRAY (unsigned varint count+1), then COMPACT_STRING for each topic.
func (p *Parser) extractTopicArray(data []byte, offset int, flexible bool) []string {
	if flexible {
		return p.extractTopicArrayCompact(data, offset)
	}
	return p.extractTopicArrayLegacy(data, offset)
}

// extractTopicArrayLegacy reads a legacy (non-flexible) topic array.
func (p *Parser) extractTopicArrayLegacy(data []byte, offset int) []string {
	if offset+4 > len(data) {
		return nil
	}

	arrayLen := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	if arrayLen <= 0 || arrayLen > 10000 {
		return nil
	}

	topics := make([]string, 0, min(int(arrayLen), 32))

	// We can't easily skip the rest of each topic structure without
	// knowing the exact schema version, so just collect the first topic.
	if arrayLen > 0 {
		if offset+2 > len(data) {
			return topics
		}
		topicLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if topicLen <= 0 || offset+int(topicLen) > len(data) {
			return topics
		}
		topics = append(topics, string(data[offset:offset+int(topicLen)]))
	}

	return topics
}

// extractTopicArrayCompact reads a compact (flexible-version) topic array.
func (p *Parser) extractTopicArrayCompact(data []byte, offset int) []string {
	arrayLen, newOffset := readCompactArrayLen(data, offset)
	if newOffset < 0 || arrayLen <= 0 {
		return nil
	}
	offset = newOffset

	if arrayLen > 10000 {
		return nil
	}

	topics := make([]string, 0, min(arrayLen, 32))

	// We can't easily skip the rest of each topic structure without
	// knowing the exact schema, so just collect the first topic.
	if arrayLen > 0 {
		topicName, newOff := readCompactString(data, offset)
		if newOff < 0 {
			return topics
		}
		if topicName != "" {
			topics = append(topics, topicName)
		}
	}

	return topics
}

// extractGroupID reads a string (group_id) at the given offset.
func (p *Parser) extractGroupID(data []byte, offset int, flexible bool) string {
	if flexible {
		s, _ := readCompactString(data, offset)
		return s
	}
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
