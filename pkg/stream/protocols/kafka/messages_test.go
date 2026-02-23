package kafka

import (
	"encoding/binary"
	"strings"
	"testing"
)

// buildRecordBatch creates a v2 record batch with the given records.
// Each record is a (key, value) pair.
func buildRecordBatch(records []struct{ key, value string }) []byte {
	// Build records first
	var recordsData []byte
	for i, rec := range records {
		var recBuf []byte
		// attributes (1 byte)
		recBuf = append(recBuf, 0)
		// timestampDelta (varint, 0)
		recBuf = append(recBuf, 0)
		// offsetDelta (varint)
		recBuf = appendSignedVarint(recBuf, int32(i))
		// keyLength
		if rec.key == "" {
			recBuf = appendSignedVarint(recBuf, -1) // null key
		} else {
			recBuf = appendSignedVarint(recBuf, int32(len(rec.key)))
			recBuf = append(recBuf, []byte(rec.key)...)
		}
		// valueLength
		if rec.value == "" {
			recBuf = appendSignedVarint(recBuf, -1) // null value
		} else {
			recBuf = appendSignedVarint(recBuf, int32(len(rec.value)))
			recBuf = append(recBuf, []byte(rec.value)...)
		}
		// headerCount (0)
		recBuf = appendSignedVarint(recBuf, 0)

		// Prepend record length
		recordsData = appendSignedVarint(recordsData, int32(len(recBuf)))
		recordsData = append(recordsData, recBuf...)
	}

	// Build batch header (61 bytes)
	header := make([]byte, 61)
	// baseOffset (8)
	binary.BigEndian.PutUint64(header[0:8], 0)
	// batchLength (4) - size of everything after baseOffset+batchLength = 61-12 + len(recordsData)
	binary.BigEndian.PutUint32(header[8:12], uint32(49+len(recordsData)))
	// partitionLeaderEpoch (4)
	binary.BigEndian.PutUint32(header[12:16], 0)
	// magic (1) = 2
	header[16] = 2
	// crc (4) - skip validation
	binary.BigEndian.PutUint32(header[17:21], 0)
	// attributes (2) - compression = 0
	binary.BigEndian.PutUint16(header[21:23], 0)
	// lastOffsetDelta (4)
	binary.BigEndian.PutUint32(header[23:27], uint32(len(records)-1))
	// baseTimestamp (8)
	binary.BigEndian.PutUint64(header[27:35], 0)
	// maxTimestamp (8)
	binary.BigEndian.PutUint64(header[35:43], 0)
	// producerId (8)
	binary.BigEndian.PutUint64(header[43:51], 0xFFFFFFFFFFFFFFFF)
	// producerEpoch (2)
	binary.BigEndian.PutUint16(header[51:53], 0xFFFF)
	// baseSequence (4)
	binary.BigEndian.PutUint32(header[53:57], 0xFFFFFFFF)
	// recordCount (4)
	binary.BigEndian.PutUint32(header[57:61], uint32(len(records)))

	result := make([]byte, 0, len(header)+len(recordsData))
	result = append(result, header...)
	result = append(result, recordsData...)
	return result
}

// appendSignedVarint appends a ZigZag-encoded signed varint.
func appendSignedVarint(buf []byte, v int32) []byte {
	uv := uint32((v << 1) ^ (v >> 31)) // ZigZag encode
	return appendUnsignedVarint(buf, uv)
}

// appendUnsignedVarint appends an unsigned varint.
func appendUnsignedVarint(buf []byte, v uint32) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	buf = append(buf, byte(v))
	return buf
}

// buildLegacyProduceRequest builds a non-flexible Produce request body
// (after the request header) with the given topic, partition, and record batch.
func buildLegacyProduceRequest(topic string, partition int32, batch []byte, apiVersion int16) []byte {
	var buf []byte

	// transactional_id (nullable string, v3+) — null = -1
	if apiVersion >= 3 {
		buf = append(buf, 0xFF, 0xFF) // -1 = null
	}

	// acks (int16) = -1 (all)
	buf = append(buf, 0xFF, 0xFF)
	// timeout_ms (int32) = 30000
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint32(tmp, 30000)
	buf = append(buf, tmp...)

	// topic array: count = 1
	binary.BigEndian.PutUint32(tmp, 1)
	buf = append(buf, tmp...)

	// topic name (int16 len + bytes)
	topicBytes := []byte(topic)
	tmp2 := make([]byte, 2)
	binary.BigEndian.PutUint16(tmp2, uint16(len(topicBytes)))
	buf = append(buf, tmp2...)
	buf = append(buf, topicBytes...)

	// partition array: count = 1
	binary.BigEndian.PutUint32(tmp, 1)
	buf = append(buf, tmp...)

	// partition_index (int32)
	binary.BigEndian.PutUint32(tmp, uint32(partition))
	buf = append(buf, tmp...)

	// record_set size (int32)
	binary.BigEndian.PutUint32(tmp, uint32(len(batch)))
	buf = append(buf, tmp...)

	// record_set bytes
	buf = append(buf, batch...)

	return buf
}

// buildFullProduceRequest wraps a body into a full Kafka request frame (size + header + body).
func buildFullProduceRequest(body []byte, apiVersion int16, correlationID int32, clientID string) []byte {
	// Header: apiKey(2) + apiVersion(2) + correlationID(4) + clientID(2+len)
	var header []byte
	h := make([]byte, 8)
	binary.BigEndian.PutUint16(h[0:2], uint16(ApiKeyProduce))
	binary.BigEndian.PutUint16(h[2:4], uint16(apiVersion))
	binary.BigEndian.PutUint32(h[4:8], uint32(correlationID))
	header = append(header, h...)

	// clientID
	cid := []byte(clientID)
	cl := make([]byte, 2)
	binary.BigEndian.PutUint16(cl, uint16(len(cid)))
	header = append(header, cl...)
	header = append(header, cid...)

	msg := make([]byte, 0, len(header)+len(body))
	msg = append(msg, header...)
	msg = append(msg, body...)

	// Size prefix
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(msg)))
	result := make([]byte, 0, 4+len(msg))
	result = append(result, size...)
	result = append(result, msg...)
	return result
}

func TestExtractRecordBatchMessages_JSONValue(t *testing.T) {
	records := []struct{ key, value string }{
		{"", `{"user":"alice","action":"login"}`},
	}
	batch := buildRecordBatch(records)
	msgs := extractRecordBatchMessages(batch, "test-topic", 0, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Value != `{"user":"alice","action":"login"}` {
		t.Errorf("unexpected value: %s", msgs[0].Value)
	}
	if msgs[0].Key != "" {
		t.Errorf("expected empty key, got: %s", msgs[0].Key)
	}
	if msgs[0].Topic != "test-topic" {
		t.Errorf("unexpected topic: %s", msgs[0].Topic)
	}
	if msgs[0].Truncated {
		t.Error("should not be truncated")
	}
}

func TestExtractRecordBatchMessages_KeyAndValue(t *testing.T) {
	records := []struct{ key, value string }{
		{"user-123", `{"name":"bob"}`},
		{"user-456", `{"name":"carol"}`},
	}
	batch := buildRecordBatch(records)
	msgs := extractRecordBatchMessages(batch, "users", 2, 10)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Key != "user-123" {
		t.Errorf("msg[0] key: %s", msgs[0].Key)
	}
	if msgs[1].Key != "user-456" {
		t.Errorf("msg[1] key: %s", msgs[1].Key)
	}
	if msgs[0].Partition != 2 {
		t.Errorf("expected partition 2, got %d", msgs[0].Partition)
	}
}

func TestExtractRecordBatchMessages_Truncation(t *testing.T) {
	largeValue := strings.Repeat("X", MaxValueSize+100)
	records := []struct{ key, value string }{
		{"k", largeValue},
	}
	batch := buildRecordBatch(records)
	msgs := extractRecordBatchMessages(batch, "big-topic", 0, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].Truncated {
		t.Error("expected truncated=true")
	}
	if len(msgs[0].Value) != MaxValueSize {
		t.Errorf("expected value length %d, got %d", MaxValueSize, len(msgs[0].Value))
	}
}

func TestExtractRecordBatchMessages_NullKey(t *testing.T) {
	records := []struct{ key, value string }{
		{"", "hello"},
	}
	batch := buildRecordBatch(records)
	msgs := extractRecordBatchMessages(batch, "t", 0, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Key != "" {
		t.Errorf("expected empty key for null, got: %s", msgs[0].Key)
	}
}

func TestExtractRecordBatchMessages_CompressedSkipped(t *testing.T) {
	batch := buildRecordBatch([]struct{ key, value string }{{"k", "v"}})
	// Set compression bits in attributes
	binary.BigEndian.PutUint16(batch[21:23], 1) // gzip
	msgs := extractRecordBatchMessages(batch, "t", 0, 10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for compressed batch, got %d", len(msgs))
	}
}

func TestExtractRecordBatchMessages_LegacyMagicSkipped(t *testing.T) {
	batch := buildRecordBatch([]struct{ key, value string }{{"k", "v"}})
	batch[16] = 1 // magic = 1 (legacy)
	msgs := extractRecordBatchMessages(batch, "t", 0, 10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for legacy magic, got %d", len(msgs))
	}
}

func TestProduceRequestMessages_Integration(t *testing.T) {
	records := []struct{ key, value string }{
		{"order-1", `{"item":"widget","qty":5}`},
	}
	batch := buildRecordBatch(records)
	body := buildLegacyProduceRequest("orders", 0, batch, 3)
	frame := buildFullProduceRequest(body, 3, 1, "test-client")

	parser := NewParser()
	parser.Append(frame)
	req, err := parser.ParseRequest()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Key != "order-1" {
		t.Errorf("key: %s", req.Messages[0].Key)
	}
	if req.Messages[0].Value != `{"item":"widget","qty":5}` {
		t.Errorf("value: %s", req.Messages[0].Value)
	}
	if req.Messages[0].Topic != "orders" {
		t.Errorf("topic: %s", req.Messages[0].Topic)
	}
}

func TestFetchResponseMessages(t *testing.T) {
	records := []struct{ key, value string }{
		{"msg-1", `{"event":"click"}`},
		{"msg-2", `{"event":"view"}`},
	}
	batch := buildRecordBatch(records)

	// Build a Fetch v4 response body (after correlation ID):
	// throttle_time_ms(4) + topics array
	var body []byte

	// throttle_time_ms = 0
	body = append(body, 0, 0, 0, 0)

	// topics array count = 1
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint32(tmp, 1)
	body = append(body, tmp...)

	// topic name
	topicBytes := []byte("events")
	tmp2 := make([]byte, 2)
	binary.BigEndian.PutUint16(tmp2, uint16(len(topicBytes)))
	body = append(body, tmp2...)
	body = append(body, topicBytes...)

	// partitions array count = 1
	binary.BigEndian.PutUint32(tmp, 1)
	body = append(body, tmp...)

	// partition_index = 0
	binary.BigEndian.PutUint32(tmp, 0)
	body = append(body, tmp...)

	// error_code = 0
	body = append(body, 0, 0)

	// high_watermark (int64) = 100
	hw := make([]byte, 8)
	binary.BigEndian.PutUint64(hw, 100)
	body = append(body, hw...)

	// last_stable_offset (int64, v4+) = 100
	body = append(body, hw...)

	// aborted_transactions (array, v4+) = empty (-1 = null? no, 0 count)
	binary.BigEndian.PutUint32(tmp, 0) // 0 aborted transactions
	body = append(body, tmp...)

	// record_set size
	binary.BigEndian.PutUint32(tmp, uint32(len(batch)))
	body = append(body, tmp...)

	// record_set
	body = append(body, batch...)

	msgs := ExtractFetchResponseMessages(body, 4)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Key != "msg-1" || msgs[0].Value != `{"event":"click"}` {
		t.Errorf("msg[0]: key=%s value=%s", msgs[0].Key, msgs[0].Value)
	}
	if msgs[1].Key != "msg-2" || msgs[1].Value != `{"event":"view"}` {
		t.Errorf("msg[1]: key=%s value=%s", msgs[1].Key, msgs[1].Value)
	}
	if msgs[0].Topic != "events" {
		t.Errorf("topic: %s", msgs[0].Topic)
	}
}

func TestMaxMessagesLimit(t *testing.T) {
	var records []struct{ key, value string }
	for range 20 {
		records = append(records, struct{ key, value string }{"k", "v"})
	}
	batch := buildRecordBatch(records)
	msgs := extractRecordBatchMessages(batch, "t", 0, MaxMessages)
	if len(msgs) != MaxMessages {
		t.Errorf("expected %d messages, got %d", MaxMessages, len(msgs))
	}
}
