package kafka

import (
	"encoding/binary"
	"testing"
)

// buildKafkaRequest builds a raw Kafka request frame
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
	if resp.ErrorCode != 3 {
		t.Errorf("expected ErrorCode 3, got %d", resp.ErrorCode)
	}
}

func TestIncompleteMessage(t *testing.T) {
	parser := NewParser()

	// Only send the first 6 bytes of a message that should be larger
	data := buildKafkaRequest(ApiKeyApiVersions, 3, 1, "test")
	parser.Append(data[:6]) // Incomplete

	_, err := parser.ParseRequest()
	if err != ErrIncomplete {
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
	if err != ErrIncomplete {
		t.Errorf("expected ErrIncomplete for empty buffer, got %v", err)
	}

	_, err = parser.ParseResponse()
	if err != ErrIncomplete {
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
	if r2.Header.CorrelationID != 200 || r2.ErrorCode != 7 {
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
