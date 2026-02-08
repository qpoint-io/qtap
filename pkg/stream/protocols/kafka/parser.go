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
	for i := range 5 { // max 5 bytes for uint32
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
	for range numTags {
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

	// Save raw body after correlation ID for post-correlation parsing (e.g., Fetch messages)
	if len(data) > 4 {
		resp.RawBody = make([]byte, len(data)-4)
		copy(resp.RawBody, data[4:])
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
		req.Messages = p.extractProduceMessages(data, offset, apiVersion, flexible)
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

// Message extraction constants
const (
	MaxMessages  = 10    // Max message samples per request
	MaxValueSize = 65536 // 64KB max per message value
	MaxKeySize   = 1024  // 1KB max per message key
)

// readSignedVarint reads a ZigZag-encoded signed varint (used in Kafka record batch records).
func readSignedVarint(data []byte, offset int) (int32, int) {
	uval, n := readUnsignedVarint(data, offset)
	if n < 0 {
		return 0, -1
	}
	// ZigZag decode
	return int32((uval >> 1) ^ -(uval & 1)), n
}

// extractRecordBatchMessages parses records from a v2 record batch (magic=2).
// Returns extracted messages and whether any were found.
func extractRecordBatchMessages(data []byte, topic string, partition int32, maxMessages int) []KafkaMessage {
	// Record batch header: baseOffset(8) + batchLength(4) + partitionLeaderEpoch(4) +
	// magic(1) + crc(4) + attributes(2) + lastOffsetDelta(4) + baseTimestamp(8) +
	// maxTimestamp(8) + producerId(8) + producerEpoch(2) + baseSequence(4) + recordCount(4)
	// = 61 bytes total
	if len(data) < 61 {
		return nil
	}

	magic := data[16]
	if magic != 2 {
		return nil // Only support v2 record batches
	}

	attributes := int16(binary.BigEndian.Uint16(data[21:23]))
	compression := attributes & 0x07
	if compression != 0 {
		return nil // Skip compressed batches in v1
	}

	recordCount := int32(binary.BigEndian.Uint32(data[57:61]))
	if recordCount <= 0 {
		return nil
	}

	offset := 61
	limit := int(recordCount)
	if limit > maxMessages {
		limit = maxMessages
	}

	var messages []KafkaMessage
	for range limit {
		if offset >= len(data) {
			break
		}

		// Record: length(varint), attributes(1), timestampDelta(varint), offsetDelta(varint),
		// keyLength(varint), key(bytes), valueLength(varint), value(bytes), headerCount(varint), headers...
		recordLen, n := readSignedVarint(data, offset)
		if n < 0 || recordLen < 0 {
			break
		}
		offset += n
		recordEnd := offset + int(recordLen)
		if recordEnd > len(data) {
			break
		}

		// attributes (1 byte)
		if offset >= recordEnd {
			break
		}
		offset++

		// timestampDelta
		_, n = readSignedVarint(data, offset)
		if n < 0 {
			break
		}
		offset += n

		// offsetDelta
		_, n = readSignedVarint(data, offset)
		if n < 0 {
			break
		}
		offset += n

		// keyLength (signed varint, -1 = null)
		keyLen, n := readSignedVarint(data, offset)
		if n < 0 {
			break
		}
		offset += n

		var key string
		if keyLen > 0 {
			if offset+int(keyLen) > recordEnd {
				break
			}
			kl := int(keyLen)
			if kl > MaxKeySize {
				kl = MaxKeySize
			}
			key = string(data[offset : offset+kl])
			offset += int(keyLen)
		}
		// keyLen == -1 means null key, key stays empty

		// valueLength (signed varint, -1 = null)
		valueLen, n := readSignedVarint(data, offset)
		if n < 0 {
			break
		}
		offset += n

		var value string
		var truncated bool
		if valueLen > 0 {
			if offset+int(valueLen) > recordEnd {
				break
			}
			vl := int(valueLen)
			if vl > MaxValueSize {
				vl = MaxValueSize
				truncated = true
			}
			value = string(data[offset : offset+vl])
		}
		// valueLen == -1 means null value, value stays empty

		messages = append(messages, KafkaMessage{
			Topic:     topic,
			Partition: partition,
			Key:       key,
			Value:     value,
			Truncated: truncated,
		})

		// Skip to next record
		offset = recordEnd
	}

	return messages
}

// extractProduceMessages extracts message samples from a Produce request body.
// This navigates the Produce request structure to find record batches.
func (p *Parser) extractProduceMessages(data []byte, offset int, apiVersion int16, flexible bool) []KafkaMessage {
	// Skip transactional_id (v3+)
	if flexible {
		_, newOffset := readCompactNullableString(data, offset)
		if newOffset < 0 {
			return nil
		}
		offset = newOffset
	} else if apiVersion >= 3 {
		offset = p.skipNullableString(data, offset)
		if offset < 0 {
			return nil
		}
	}

	// Skip acks (2) + timeout (4)
	offset += 6
	if offset > len(data) {
		return nil
	}

	// Topic array
	var topicCount int
	if flexible {
		tc, newOffset := readCompactArrayLen(data, offset)
		if newOffset < 0 || tc <= 0 {
			return nil
		}
		topicCount = tc
		offset = newOffset
	} else {
		if offset+4 > len(data) {
			return nil
		}
		topicCount = int(int32(binary.BigEndian.Uint32(data[offset : offset+4])))
		offset += 4
		if topicCount <= 0 {
			return nil
		}
	}

	var messages []KafkaMessage

	for t := 0; t < topicCount && len(messages) < MaxMessages; t++ {
		// Topic name
		var topicName string
		if flexible {
			var newOffset int
			topicName, newOffset = readCompactString(data, offset)
			if newOffset < 0 {
				return messages
			}
			offset = newOffset
		} else {
			if offset+2 > len(data) {
				return messages
			}
			tl := int(int16(binary.BigEndian.Uint16(data[offset : offset+2])))
			offset += 2
			if tl < 0 || offset+tl > len(data) {
				return messages
			}
			topicName = string(data[offset : offset+tl])
			offset += tl
		}

		// Partition array
		var partCount int
		if flexible {
			pc, newOffset := readCompactArrayLen(data, offset)
			if newOffset < 0 || pc <= 0 {
				return messages
			}
			partCount = pc
			offset = newOffset
		} else {
			if offset+4 > len(data) {
				return messages
			}
			partCount = int(int32(binary.BigEndian.Uint32(data[offset : offset+4])))
			offset += 4
			if partCount <= 0 {
				return messages
			}
		}

		for pt := 0; pt < partCount && len(messages) < MaxMessages; pt++ {
			// partition_index (int32)
			if offset+4 > len(data) {
				return messages
			}
			partitionID := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
			offset += 4

			// record_set size
			var recordSetSize int
			if flexible {
				// COMPACT_BYTES: unsigned varint length+1
				rawLen, n := readUnsignedVarint(data, offset)
				if n < 0 {
					return messages
				}
				offset += n
				if rawLen == 0 {
					continue // null
				}
				recordSetSize = int(rawLen - 1)
			} else {
				if offset+4 > len(data) {
					return messages
				}
				recordSetSize = int(int32(binary.BigEndian.Uint32(data[offset : offset+4])))
				offset += 4
				if recordSetSize <= 0 {
					continue
				}
			}

			if offset+recordSetSize > len(data) {
				return messages
			}

			// Parse record batch within the record set
			batchData := data[offset : offset+recordSetSize]
			msgs := extractRecordBatchMessages(batchData, topicName, partitionID, MaxMessages-len(messages))
			messages = append(messages, msgs...)

			offset += recordSetSize

			// Skip tagged fields for this partition (flexible)
			if flexible {
				newOffset := skipTaggedFields(data, offset)
				if newOffset < 0 {
					return messages
				}
				offset = newOffset
			}
		}

		// Skip tagged fields for this topic (flexible)
		if flexible {
			newOffset := skipTaggedFields(data, offset)
			if newOffset < 0 {
				return messages
			}
			offset = newOffset
		}
	}

	return messages
}

// ExtractFetchResponseMessages extracts message samples from a Fetch response body.
// Called after correlation when we know the API key is Fetch.
// body is the raw response data after the 4-byte correlation ID.
func ExtractFetchResponseMessages(body []byte, apiVersion int16) []KafkaMessage {
	flexible := isFlexibleVersion(ApiKeyFetch, apiVersion)

	offset := skipFetchResponseHeader(body, apiVersion, flexible)
	if offset < 0 || offset > len(body) {
		return nil
	}

	topicCount, newOff := readArrayLen(body, offset, flexible)
	if topicCount <= 0 || newOff < 0 {
		return nil
	}
	offset = newOff

	var messages []KafkaMessage
	for t := 0; t < topicCount && len(messages) < MaxMessages; t++ {
		topicName, off := readFetchTopicName(body, offset, apiVersion, flexible)
		if off < 0 {
			return messages
		}
		offset = off

		partCount, off2 := readArrayLen(body, offset, flexible)
		if partCount <= 0 || off2 < 0 {
			return messages
		}
		offset = off2

		for pt := 0; pt < partCount && len(messages) < MaxMessages; pt++ {
			partitionID, recordSetData, off3 := parseFetchPartition(body, offset, apiVersion, flexible)
			if off3 < 0 {
				return messages
			}
			offset = off3
			if len(recordSetData) > 0 {
				msgs := extractRecordBatchMessages(recordSetData, topicName, partitionID, MaxMessages-len(messages))
				messages = append(messages, msgs...)
			}
		}

		if flexible {
			off4 := skipTaggedFields(body, offset)
			if off4 < 0 {
				return messages
			}
			offset = off4
		}
	}

	return messages
}

// skipFetchResponseHeader skips the fixed fields at the start of a Fetch response body.
func skipFetchResponseHeader(body []byte, apiVersion int16, flexible bool) int {
	offset := 0
	if flexible {
		newOffset := skipTaggedFields(body, offset)
		if newOffset < 0 {
			return -1
		}
		offset = newOffset
	}
	if apiVersion >= 1 {
		offset += 4 // throttle_time_ms
	}
	if apiVersion >= 7 {
		offset += 6 // error_code(2) + session_id(4)
	}
	return offset
}

// readArrayLen reads an array length (legacy or compact).
func readArrayLen(data []byte, offset int, flexible bool) (int, int) {
	if flexible {
		return readCompactArrayLen(data, offset)
	}
	if offset+4 > len(data) {
		return -1, -1
	}
	n := int(int32(binary.BigEndian.Uint32(data[offset : offset+4])))
	return n, offset + 4
}

// readFetchTopicName reads the topic identifier from a Fetch response topic entry.
func readFetchTopicName(data []byte, offset int, apiVersion int16, flexible bool) (string, int) {
	switch {
	case apiVersion >= 13:
		if offset+16 > len(data) {
			return "", -1
		}
		return "<uuid>", offset + 16
	case flexible:
		return readCompactString(data, offset)
	default:
		if offset+2 > len(data) {
			return "", -1
		}
		tl := int(int16(binary.BigEndian.Uint16(data[offset : offset+2])))
		offset += 2
		if tl < 0 || offset+tl > len(data) {
			return "", -1
		}
		return string(data[offset : offset+tl]), offset + tl
	}
}

// parseFetchPartition parses a single partition from a Fetch response.
func parseFetchPartition(data []byte, offset int, apiVersion int16, flexible bool) (int32, []byte, int) {
	// partition_index(4) + error_code(2) + high_watermark(8) = 14
	if offset+14 > len(data) {
		return 0, nil, -1
	}
	partitionID := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 14

	if apiVersion >= 4 {
		offset += 8 // last_stable_offset
	}
	if apiVersion >= 5 {
		offset += 8 // log_start_offset
	}
	if apiVersion >= 4 {
		offset = skipAbortedTransactions(data, offset, flexible)
		if offset < 0 {
			return 0, nil, -1
		}
	}
	if apiVersion >= 11 {
		offset += 4 // preferred_read_replica
	}
	if offset > len(data) {
		return 0, nil, -1
	}

	recordSetData, newOffset := readBytesField(data, offset, flexible)
	if newOffset < 0 {
		return 0, nil, -1
	}
	offset = newOffset

	if flexible {
		off := skipTaggedFields(data, offset)
		if off < 0 {
			return 0, nil, -1
		}
		offset = off
	}

	return partitionID, recordSetData, offset
}

// skipAbortedTransactions skips the aborted_transactions array.
func skipAbortedTransactions(data []byte, offset int, flexible bool) int {
	count, newOffset := readArrayLen(data, offset, flexible)
	if newOffset < 0 {
		return -1
	}
	offset = newOffset
	for range count {
		offset += 16 // producer_id(8) + first_offset(8)
		if flexible {
			off := skipTaggedFields(data, offset)
			if off < 0 {
				return -1
			}
			offset = off
		}
	}
	return offset
}

// readBytesField reads a BYTES/COMPACT_BYTES field.
func readBytesField(data []byte, offset int, flexible bool) ([]byte, int) {
	if flexible {
		rawLen, n := readUnsignedVarint(data, offset)
		if n < 0 {
			return nil, -1
		}
		offset += n
		if rawLen == 0 {
			return nil, offset // null
		}
		size := int(rawLen - 1)
		if offset+size > len(data) {
			return nil, -1
		}
		return data[offset : offset+size], offset + size
	}
	if offset+4 > len(data) {
		return nil, -1
	}
	size := int(int32(binary.BigEndian.Uint32(data[offset : offset+4])))
	offset += 4
	if size <= 0 {
		return nil, offset
	}
	if offset+size > len(data) {
		return nil, -1
	}
	return data[offset : offset+size], offset + size
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
