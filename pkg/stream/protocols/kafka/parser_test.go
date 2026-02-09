package kafka

import (
	"encoding/binary"
	"errors"
	"testing"
)

// buildKafkaRequest builds a raw Kafka request frame (legacy, non-flexible)
func buildKafkaRequest(apiKey, apiVersion int16, correlationID int32, clientID string) []byte {
	// Calculate sizes
	clientIDBytes := []byte(clientID)
	// Header: apiKey(2) + apiVersion(2) + correlationID(4) + clientIDLen(2) + clientID
	headerSize := 2 + 2 + 4 + 2 + len(clientIDBytes)
	messageSize := headerSize

	buf := make([]byte, 4+messageSize)
	binary.BigEndian.PutUint32(buf[0:4], uint32(messageSize))
	binary.BigEndian.PutUint16(buf[4:6], uint16(apiKey))
	binary.BigEndian.PutUint16(buf[6:8], uint16(apiVersion))
	binary.BigEndian.PutUint32(buf[8:12], uint32(correlationID))
	binary.BigEndian.PutUint16(buf[12:14], uint16(len(clientIDBytes)))
	copy(buf[14:], clientIDBytes)

	return buf
}

// buildFlexibleKafkaRequest builds a raw Kafka request frame with flexible version header.
// Flexible header = apiKey(2) + apiVersion(2) + correlationID(4) + clientID(INT16 len + data) + tagged_fields(varint).
// The body uses compact encoding.
func buildFlexibleKafkaRequest(apiKey, apiVersion int16, correlationID int32, clientID string, body []byte) []byte {
	clientIDBytes := []byte(clientID)

	// Header: apiKey(2) + apiVersion(2) + correlationID(4) + clientIDLen(2) + clientID + tagged_fields(1 byte: 0x00)
	headerSize := 2 + 2 + 4 + 2 + len(clientIDBytes) + 1 // +1 for tagged fields (0 tags)
	messageSize := headerSize + len(body)

	buf := make([]byte, 4+messageSize)
	offset := 0
	binary.BigEndian.PutUint32(buf[offset:], uint32(messageSize))
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(apiKey))
	offset += 2
	binary.BigEndian.PutUint16(buf[offset:], uint16(apiVersion))
	offset += 2
	binary.BigEndian.PutUint32(buf[offset:], uint32(correlationID))
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(clientIDBytes)))
	offset += 2
	copy(buf[offset:], clientIDBytes)
	offset += len(clientIDBytes)
	buf[offset] = 0x00 // tagged fields: 0 tags
	offset++
	copy(buf[offset:], body)

	return buf
}

// putUnsignedVarint writes an unsigned varint to buf at offset, returning bytes written.
func putUnsignedVarint(buf []byte, offset int, value uint32) int {
	i := 0
	for value >= 0x80 {
		buf[offset+i] = byte(value&0x7F) | 0x80
		value >>= 7
		i++
	}
	buf[offset+i] = byte(value)
	return i + 1
}

// buildCompactString builds a COMPACT_STRING (unsigned varint(len+1) + data).
func buildCompactString(s string) []byte {
	data := []byte(s)
	lenVal := uint32(len(data) + 1) // compact string: len+1
	buf := make([]byte, 5+len(data))
	n := putUnsignedVarint(buf, 0, lenVal)
	copy(buf[n:], data)
	return buf[:n+len(data)]
}

// buildCompactArrayHeader builds the COMPACT_ARRAY length prefix (unsigned varint(count+1)).
func buildCompactArrayHeader(count int) []byte {
	buf := make([]byte, 5)
	n := putUnsignedVarint(buf, 0, uint32(count+1))
	return buf[:n]
}

// buildKafkaResponse builds a raw Kafka response frame
func buildKafkaResponse(correlationID int32, errorCode int16) []byte {
	// response: correlationID(4) + errorCode(2)
	messageSize := 6
	buf := make([]byte, 4+messageSize)
	binary.BigEndian.PutUint32(buf[0:4], uint32(messageSize))
	binary.BigEndian.PutUint32(buf[4:8], uint32(correlationID))
	binary.BigEndian.PutUint16(buf[8:10], uint16(errorCode))
	return buf
}

func TestParseApiVersionsRequest(t *testing.T) {
	parser := NewParser()

	data := buildKafkaRequest(ApiKeyApiVersions, 3, 1, "test-client")
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyApiVersions {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyApiVersions, req.Header.ApiKey)
	}
	if req.Header.ApiVersion != 3 {
		t.Errorf("expected ApiVersion 3, got %d", req.Header.ApiVersion)
	}
	if req.Header.CorrelationID != 1 {
		t.Errorf("expected CorrelationID 1, got %d", req.Header.CorrelationID)
	}
	if req.Header.ClientID != "test-client" {
		t.Errorf("expected ClientID 'test-client', got '%s'", req.Header.ClientID)
	}
}

func TestParseMetadataRequest(t *testing.T) {
	parser := NewParser()

	data := buildKafkaRequest(ApiKeyMetadata, 1, 2, "my-producer")
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyMetadata {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyMetadata, req.Header.ApiKey)
	}
	if req.Header.ClientID != "my-producer" {
		t.Errorf("expected ClientID 'my-producer', got '%s'", req.Header.ClientID)
	}
}

func TestParseProduceRequest(t *testing.T) {
	parser := NewParser()

	data := buildKafkaRequest(ApiKeyProduce, 0, 3, "producer-1")
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyProduce {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyProduce, req.Header.ApiKey)
	}
	if req.Header.CorrelationID != 3 {
		t.Errorf("expected CorrelationID 3, got %d", req.Header.CorrelationID)
	}
}

func TestParseFetchRequest(t *testing.T) {
	parser := NewParser()

	data := buildKafkaRequest(ApiKeyFetch, 4, 10, "consumer-1")
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyFetch {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyFetch, req.Header.ApiKey)
	}
	if req.Header.ClientID != "consumer-1" {
		t.Errorf("expected ClientID 'consumer-1', got '%s'", req.Header.ClientID)
	}
}

func TestParseResponseAndCorrelation(t *testing.T) {
	parser := NewParser()

	data := buildKafkaResponse(1, 0)
	parser.Append(data)

	resp, err := parser.ParseResponse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Header.CorrelationID != 1 {
		t.Errorf("expected CorrelationID 1, got %d", resp.Header.CorrelationID)
	}
	if resp.ErrorCode != 0 {
		t.Errorf("expected ErrorCode 0, got %d", resp.ErrorCode)
	}
}

func TestParseResponseWithError(t *testing.T) {
	parser := NewParser()

	data := buildKafkaResponse(5, 3) // UNKNOWN_TOPIC_OR_PARTITION
	parser.Append(data)

	resp, err := parser.ParseResponse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Header.CorrelationID != 5 {
		t.Errorf("expected CorrelationID 5, got %d", resp.Header.CorrelationID)
	}
	if resp.ErrorCode != 0 {
		t.Errorf("expected ErrorCode 0 (deferred extraction), got %d", resp.ErrorCode)
	}
}

func TestExtractResponseErrorCodeProduceV12(t *testing.T) {
	// Raw body after correlation_id for Produce v12 (flexible):
	// tagged_fields(0), throttle_time_ms(4), responses[1], topic "t", partitions[1], partition=0, error_code=2
	body := []byte{
		0x00,                   // tagged fields (header v1)
		0x00, 0x00, 0x00, 0x00, // throttle_time_ms
		0x02,      // compact array len=1
		0x02, 't', // compact string "t"
		0x02,                   // compact array len=1
		0x00, 0x00, 0x00, 0x00, // partition index
		0x00, 0x02, // error_code = CORRUPT_MESSAGE
	}

	code, ok := ExtractResponseErrorCode(body, ApiKeyProduce, 12)
	if !ok {
		t.Fatalf("expected produce v12 error extraction to succeed")
	}
	if code != 2 {
		t.Fatalf("expected error code 2, got %d", code)
	}
}

func TestIncompleteMessage(t *testing.T) {
	parser := NewParser()

	// Only send the first 6 bytes of a message that should be larger
	data := buildKafkaRequest(ApiKeyApiVersions, 3, 1, "test")
	parser.Append(data[:6]) // Incomplete

	_, err := parser.ParseRequest()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("expected ErrIncomplete, got %v", err)
	}

	// Now send the rest
	parser.Append(data[6:])

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error after completing data: %v", err)
	}

	if req.Header.ApiKey != ApiKeyApiVersions {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyApiVersions, req.Header.ApiKey)
	}
}

func TestMultipleMessagesInOneBuffer(t *testing.T) {
	parser := NewParser()

	msg1 := buildKafkaRequest(ApiKeyApiVersions, 3, 1, "client1")
	msg2 := buildKafkaRequest(ApiKeyMetadata, 1, 2, "client2")

	combined := make([]byte, len(msg1)+len(msg2))
	copy(combined, msg1)
	copy(combined[len(msg1):], msg2)
	parser.Append(combined)

	req1, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error parsing first message: %v", err)
	}
	if req1.Header.ApiKey != ApiKeyApiVersions {
		t.Errorf("first message: expected ApiKey %d, got %d", ApiKeyApiVersions, req1.Header.ApiKey)
	}

	req2, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error parsing second message: %v", err)
	}
	if req2.Header.ApiKey != ApiKeyMetadata {
		t.Errorf("second message: expected ApiKey %d, got %d", ApiKeyMetadata, req2.Header.ApiKey)
	}
}

func TestEmptyBuffer(t *testing.T) {
	parser := NewParser()

	_, err := parser.ParseRequest()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("expected ErrIncomplete for empty buffer, got %v", err)
	}

	_, err = parser.ParseResponse()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("expected ErrIncomplete for empty buffer, got %v", err)
	}
}

func TestApiKeyName(t *testing.T) {
	tests := []struct {
		apiKey   int16
		expected string
	}{
		{0, "Produce"},
		{1, "Fetch"},
		{2, "ListOffsets"},
		{3, "Metadata"},
		{18, "ApiVersions"},
		{11, "JoinGroup"},
		{14, "SyncGroup"},
		{8, "OffsetCommit"},
		{99, "Unknown(99)"},
	}

	for _, tt := range tests {
		name := ApiKeyName(tt.apiKey)
		if name != tt.expected {
			t.Errorf("ApiKeyName(%d) = %s, want %s", tt.apiKey, name, tt.expected)
		}
	}
}

func TestKafkaErrorName(t *testing.T) {
	tests := []struct {
		code     int16
		expected string
	}{
		{0, "NONE"},
		{3, "UNKNOWN_TOPIC_OR_PARTITION"},
		{7, "REQUEST_TIMED_OUT"},
		{35, "UNSUPPORTED_VERSION"},
		{-1, "UNKNOWN_SERVER_ERROR"},
		{999, "UNKNOWN_ERROR(999)"},
	}

	for _, tt := range tests {
		name := KafkaErrorName(tt.code)
		if name != tt.expected {
			t.Errorf("KafkaErrorName(%d) = %s, want %s", tt.code, name, tt.expected)
		}
	}
}

func TestParseRequestWithNullClientID(t *testing.T) {
	parser := NewParser()

	// Build a request with null client_id (-1)
	messageSize := 2 + 2 + 4 + 2 // apiKey + apiVersion + correlationID + clientIDLen(-1)
	buf := make([]byte, 4+messageSize)
	binary.BigEndian.PutUint32(buf[0:4], uint32(messageSize))
	binary.BigEndian.PutUint16(buf[4:6], uint16(ApiKeyApiVersions))
	binary.BigEndian.PutUint16(buf[6:8], uint16(0))
	binary.BigEndian.PutUint32(buf[8:12], uint32(42))
	binary.BigEndian.PutUint16(buf[12:14], uint16(0xFFFF)) // -1 as uint16

	parser.Append(buf)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ClientID != "" {
		t.Errorf("expected empty ClientID for null, got '%s'", req.Header.ClientID)
	}
	if req.Header.CorrelationID != 42 {
		t.Errorf("expected CorrelationID 42, got %d", req.Header.CorrelationID)
	}
}

func TestProduceRequestWithTopics(t *testing.T) {
	parser := NewParser()

	// Build a Produce v0 request with topic data:
	// After client_id: acks(2) + timeout(4) + topic_array
	clientID := "prod"

	// Body after clientID: acks(2) + timeout(4) + array_len(4) + topic_name_len(2) + topic_name
	topicName := "test-topic"
	bodySize := 2 + len(clientID) + 2 + 4 + 4 + 2 + len(topicName)
	headerSize := 2 + 2 + 4 // apiKey + apiVersion + correlationID
	messageSize := headerSize + bodySize

	buf := make([]byte, 4+messageSize)
	offset := 0

	// Length
	binary.BigEndian.PutUint32(buf[offset:], uint32(messageSize))
	offset += 4

	// Header
	binary.BigEndian.PutUint16(buf[offset:], uint16(ApiKeyProduce))
	offset += 2
	binary.BigEndian.PutUint16(buf[offset:], uint16(0)) // v0
	offset += 2
	binary.BigEndian.PutUint32(buf[offset:], uint32(10))
	offset += 4

	// Client ID
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(clientID)))
	offset += 2
	copy(buf[offset:], clientID)
	offset += len(clientID)

	// Acks
	binary.BigEndian.PutUint16(buf[offset:], uint16(1))
	offset += 2

	// Timeout
	binary.BigEndian.PutUint32(buf[offset:], uint32(30000))
	offset += 4

	// Topic array
	binary.BigEndian.PutUint32(buf[offset:], uint32(1))
	offset += 4

	// Topic name
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(topicName)))
	offset += 2
	copy(buf[offset:], topicName)

	parser.Append(buf)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyProduce {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyProduce, req.Header.ApiKey)
	}
	if len(req.Topics) == 0 {
		t.Fatal("expected at least one topic, got none")
	}
	if req.Topics[0] != "test-topic" {
		t.Errorf("expected topic 'test-topic', got '%s'", req.Topics[0])
	}
}

func TestGroupIDExtraction(t *testing.T) {
	parser := NewParser()

	// Build a JoinGroup request: after client_id comes the group_id
	clientID := "client"
	groupID := "my-consumer-group"

	bodySize := 2 + len(clientID) + 2 + len(groupID)
	headerSize := 2 + 2 + 4
	messageSize := headerSize + bodySize

	buf := make([]byte, 4+messageSize)
	offset := 0

	binary.BigEndian.PutUint32(buf[offset:], uint32(messageSize))
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(ApiKeyJoinGroup))
	offset += 2
	binary.BigEndian.PutUint16(buf[offset:], uint16(0))
	offset += 2
	binary.BigEndian.PutUint32(buf[offset:], uint32(20))
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(clientID)))
	offset += 2
	copy(buf[offset:], clientID)
	offset += len(clientID)
	// group_id
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(groupID)))
	offset += 2
	copy(buf[offset:], groupID)

	parser.Append(buf)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyJoinGroup {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyJoinGroup, req.Header.ApiKey)
	}
	if req.GroupID != "my-consumer-group" {
		t.Errorf("expected GroupID 'my-consumer-group', got '%s'", req.GroupID)
	}
}

func TestMultipleResponseCorrelation(t *testing.T) {
	parser := NewParser()

	resp1 := buildKafkaResponse(100, 0)
	resp2 := buildKafkaResponse(200, 7) // REQUEST_TIMED_OUT
	resp3 := buildKafkaResponse(300, 0)

	combined := make([]byte, len(resp1)+len(resp2)+len(resp3))
	copy(combined, resp1)
	copy(combined[len(resp1):], resp2)
	copy(combined[len(resp1)+len(resp2):], resp3)
	parser.Append(combined)

	r1, err := parser.ParseResponse()
	if err != nil {
		t.Fatalf("error parsing resp1: %v", err)
	}
	if r1.Header.CorrelationID != 100 || r1.ErrorCode != 0 {
		t.Errorf("resp1: corrID=%d errCode=%d", r1.Header.CorrelationID, r1.ErrorCode)
	}

	r2, err := parser.ParseResponse()
	if err != nil {
		t.Fatalf("error parsing resp2: %v", err)
	}
	if r2.Header.CorrelationID != 200 || r2.ErrorCode != 0 {
		t.Errorf("resp2: corrID=%d errCode=%d", r2.Header.CorrelationID, r2.ErrorCode)
	}

	r3, err := parser.ParseResponse()
	if err != nil {
		t.Fatalf("error parsing resp3: %v", err)
	}
	if r3.Header.CorrelationID != 300 || r3.ErrorCode != 0 {
		t.Errorf("resp3: corrID=%d errCode=%d", r3.Header.CorrelationID, r3.ErrorCode)
	}
}

// --- Flexible version tests ---

func TestReadUnsignedVarint(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected uint32
		bytes    int
	}{
		{"zero", []byte{0x00}, 0, 1},
		{"one", []byte{0x01}, 1, 1},
		{"127", []byte{0x7F}, 127, 1},
		{"128", []byte{0x80, 0x01}, 128, 2},
		{"300", []byte{0xAC, 0x02}, 300, 2},
		{"16384", []byte{0x80, 0x80, 0x01}, 16384, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, n := readUnsignedVarint(tt.data, 0)
			if n != tt.bytes {
				t.Errorf("expected %d bytes, got %d", tt.bytes, n)
			}
			if val != tt.expected {
				t.Errorf("expected value %d, got %d", tt.expected, val)
			}
		})
	}
}

func TestReadCompactString(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{"null", []byte{0x00}, ""},
		{"empty", []byte{0x01}, ""},
		{"hello", append([]byte{0x06}, []byte("hello")...), "hello"},
		{"test-topic", append([]byte{0x0B}, []byte("test-topic")...), "test-topic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, off := readCompactString(tt.data, 0)
			if off < 0 {
				t.Fatalf("readCompactString returned error offset")
			}
			if s != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, s)
			}
		})
	}
}

func TestReadCompactArrayLen(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected int
	}{
		{"null", []byte{0x00}, -1},
		{"empty", []byte{0x01}, 0},
		{"one", []byte{0x02}, 1},
		{"five", []byte{0x06}, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, off := readCompactArrayLen(tt.data, 0)
			if off < 0 {
				t.Fatalf("readCompactArrayLen returned error offset")
			}
			if n != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, n)
			}
		})
	}
}

func TestIsFlexibleVersion(t *testing.T) {
	tests := []struct {
		apiKey     int16
		apiVersion int16
		expected   bool
	}{
		{ApiKeyProduce, 8, false},
		{ApiKeyProduce, 9, true},
		{ApiKeyProduce, 10, true},
		{ApiKeyFetch, 11, false},
		{ApiKeyFetch, 12, true},
		{ApiKeyMetadata, 8, false},
		{ApiKeyMetadata, 9, true},
		{ApiKeyCreateTopics, 4, false},
		{ApiKeyCreateTopics, 5, true},
		{ApiKeyApiVersions, 3, false}, // special case
		{ApiKeyJoinGroup, 5, false},
		{ApiKeyJoinGroup, 6, true},
	}

	for _, tt := range tests {
		result := isFlexibleVersion(tt.apiKey, tt.apiVersion)
		if result != tt.expected {
			t.Errorf("isFlexibleVersion(%d, %d) = %v, want %v", tt.apiKey, tt.apiVersion, result, tt.expected)
		}
	}
}

func TestFlexibleProduceRequestWithTopics(t *testing.T) {
	// Build a Produce v9 (flexible) request body:
	// transactional_id (COMPACT_NULLABLE_STRING: null = 0x00)
	// acks (INT16)
	// timeout_ms (INT32)
	// topic_data (COMPACT_ARRAY of topics, each starting with COMPACT_STRING name)

	topicName := "flex-topic"

	var body []byte
	// transactional_id: null (compact null = 0x00)
	body = append(body, 0x00)
	// acks: 1
	body = append(body, 0x00, 0x01)
	// timeout: 30000
	body = append(body, 0x00, 0x00, 0x75, 0x30)
	// topic_data array: 1 element (compact: count+1 = 2)
	body = append(body, buildCompactArrayHeader(1)...)
	// topic name (compact string)
	body = append(body, buildCompactString(topicName)...)

	parser := NewParser()
	data := buildFlexibleKafkaRequest(ApiKeyProduce, 9, 100, "prod-client", body)
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyProduce {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyProduce, req.Header.ApiKey)
	}
	if req.Header.ApiVersion != 9 {
		t.Errorf("expected ApiVersion 9, got %d", req.Header.ApiVersion)
	}
	if req.Header.ClientID != "prod-client" {
		t.Errorf("expected ClientID 'prod-client', got '%s'", req.Header.ClientID)
	}
	if len(req.Topics) == 0 {
		t.Fatal("expected at least one topic, got none")
	}
	if req.Topics[0] != "flex-topic" {
		t.Errorf("expected topic 'flex-topic', got '%s'", req.Topics[0])
	}
}

func TestFlexibleMetadataRequestWithTopics(t *testing.T) {
	// Build a Metadata v9 (flexible) request body:
	// topics (COMPACT_ARRAY of MetadataRequestTopic, each starting with COMPACT_STRING name)

	topicName := "my-metadata-topic"

	var body []byte
	// topics array: 1 element (compact: count+1 = 2)
	body = append(body, buildCompactArrayHeader(1)...)
	// topic name (compact string)
	body = append(body, buildCompactString(topicName)...)

	parser := NewParser()
	data := buildFlexibleKafkaRequest(ApiKeyMetadata, 9, 200, "meta-client", body)
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyMetadata {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyMetadata, req.Header.ApiKey)
	}
	if len(req.Topics) == 0 {
		t.Fatal("expected at least one topic, got none")
	}
	if req.Topics[0] != "my-metadata-topic" {
		t.Errorf("expected topic 'my-metadata-topic', got '%s'", req.Topics[0])
	}
}

func TestFlexibleCreateTopicsRequestWithTopics(t *testing.T) {
	// Build a CreateTopics v5 (flexible) request body:
	// topics (COMPACT_ARRAY of CreatableTopic, each starting with COMPACT_STRING name)

	topicName := "new-topic"

	var body []byte
	// topics array: 1 element (compact: count+1 = 2)
	body = append(body, buildCompactArrayHeader(1)...)
	// topic name (compact string)
	body = append(body, buildCompactString(topicName)...)

	parser := NewParser()
	data := buildFlexibleKafkaRequest(ApiKeyCreateTopics, 5, 300, "admin-client", body)
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyCreateTopics {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyCreateTopics, req.Header.ApiKey)
	}
	if len(req.Topics) == 0 {
		t.Fatal("expected at least one topic, got none")
	}
	if req.Topics[0] != "new-topic" {
		t.Errorf("expected topic 'new-topic', got '%s'", req.Topics[0])
	}
}

func TestFlexibleFetchRequestWithTopics(t *testing.T) {
	// Build a Fetch v12 (flexible) request body:
	// replica_id(4) + max_wait_time(4) + min_bytes(4) + max_bytes(4) + isolation_level(1) + session_id(4) + session_epoch(4) + [topics]

	topicName := "fetch-topic"

	var body []byte
	// replica_id: -1 (consumer)
	body = append(body, 0xFF, 0xFF, 0xFF, 0xFF)
	// max_wait_time: 500
	body = append(body, 0x00, 0x00, 0x01, 0xF4)
	// min_bytes: 1
	body = append(body, 0x00, 0x00, 0x00, 0x01)
	// max_bytes (v3+): 1048576
	body = append(body, 0x00, 0x10, 0x00, 0x00)
	// isolation_level (v4+): 0
	body = append(body, 0x00)
	// session_id (v7+): 0
	body = append(body, 0x00, 0x00, 0x00, 0x00)
	// session_epoch (v7+): -1
	body = append(body, 0xFF, 0xFF, 0xFF, 0xFF)
	// topics array: 1 element (compact: count+1 = 2)
	body = append(body, buildCompactArrayHeader(1)...)
	// topic name (compact string)
	body = append(body, buildCompactString(topicName)...)

	parser := NewParser()
	data := buildFlexibleKafkaRequest(ApiKeyFetch, 12, 400, "consumer-client", body)
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyFetch {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyFetch, req.Header.ApiKey)
	}
	if len(req.Topics) == 0 {
		t.Fatal("expected at least one topic, got none")
	}
	if req.Topics[0] != "fetch-topic" {
		t.Errorf("expected topic 'fetch-topic', got '%s'", req.Topics[0])
	}
}

func TestFlexibleMetadataNullTopics(t *testing.T) {
	// Metadata v9+ with null topics array (means "all topics")
	var body []byte
	// topics: null (compact null = 0x00)
	body = append(body, 0x00)

	parser := NewParser()
	data := buildFlexibleKafkaRequest(ApiKeyMetadata, 9, 500, "client", body)
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyMetadata {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyMetadata, req.Header.ApiKey)
	}
	// Null topics array should result in no topics extracted
	if len(req.Topics) != 0 {
		t.Errorf("expected no topics for null array, got %v", req.Topics)
	}
}

func TestFlexibleJoinGroupWithGroupID(t *testing.T) {
	// JoinGroup v6 (flexible): group_id is first field, encoded as COMPACT_STRING
	groupID := "my-flex-group"

	var body []byte
	body = append(body, buildCompactString(groupID)...)

	parser := NewParser()
	data := buildFlexibleKafkaRequest(ApiKeyJoinGroup, 6, 600, "client", body)
	parser.Append(data)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyJoinGroup {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyJoinGroup, req.Header.ApiKey)
	}
	if req.GroupID != "my-flex-group" {
		t.Errorf("expected GroupID 'my-flex-group', got '%s'", req.GroupID)
	}
}

func TestSkipTaggedFields(t *testing.T) {
	// Test with 0 tags
	data := []byte{0x00}
	offset := skipTaggedFields(data, 0)
	if offset != 1 {
		t.Errorf("expected offset 1 for 0 tags, got %d", offset)
	}

	// Test with 1 tag: tag_type=0, data_len=3, data=[0x01, 0x02, 0x03]
	data = []byte{0x01, 0x00, 0x03, 0x01, 0x02, 0x03}
	offset = skipTaggedFields(data, 0)
	if offset != 6 {
		t.Errorf("expected offset 6 for 1 tag with 3 bytes, got %d", offset)
	}
}

func TestLegacyProduceV3WithNullTransactionalID(t *testing.T) {
	// Produce v3 (non-flexible): transactional_id is nullable string (INT16 -1),
	// then acks(2) + timeout(4) + topic_array (INT32 array len + INT16 string)
	parser := NewParser()
	clientID := "txn-prod"
	topicName := "txn-topic"

	// Calculate sizes carefully
	// Header: apiKey(2) + apiVersion(2) + correlationID(4) = 8
	// ClientID: len(2) + data
	// Body: transactional_id(2 for null) + acks(2) + timeout(4) + array_len(4) + topic_len(2) + topic
	bodyAfterClient := 2 + 2 + 4 + 4 + 2 + len(topicName)
	messageSize := 8 + 2 + len(clientID) + bodyAfterClient

	buf := make([]byte, 4+messageSize)
	offset := 0

	binary.BigEndian.PutUint32(buf[offset:], uint32(messageSize))
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(ApiKeyProduce))
	offset += 2
	binary.BigEndian.PutUint16(buf[offset:], uint16(3)) // v3
	offset += 2
	binary.BigEndian.PutUint32(buf[offset:], uint32(50))
	offset += 4
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(clientID)))
	offset += 2
	copy(buf[offset:], clientID)
	offset += len(clientID)
	// transactional_id: null (-1)
	binary.BigEndian.PutUint16(buf[offset:], uint16(0xFFFF))
	offset += 2
	// acks: -1
	binary.BigEndian.PutUint16(buf[offset:], uint16(0xFFFF))
	offset += 2
	// timeout: 30000
	binary.BigEndian.PutUint32(buf[offset:], uint32(30000))
	offset += 4
	// topic array: 1 element
	binary.BigEndian.PutUint32(buf[offset:], uint32(1))
	offset += 4
	// topic name
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(topicName)))
	offset += 2
	copy(buf[offset:], topicName)

	parser.Append(buf)

	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.ApiKey != ApiKeyProduce {
		t.Errorf("expected ApiKey %d, got %d", ApiKeyProduce, req.Header.ApiKey)
	}
	if len(req.Topics) == 0 {
		t.Fatal("expected at least one topic, got none")
	}
	if req.Topics[0] != "txn-topic" {
		t.Errorf("expected topic 'txn-topic', got '%s'", req.Topics[0])
	}
}
