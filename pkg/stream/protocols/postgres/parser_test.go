package postgres

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseMessage(t *testing.T) {
	p := NewParser()

	// Build a Query message: type='Q', length=4+len("SELECT 1\x00"), payload="SELECT 1\x00"
	sql := "SELECT 1"
	payload := append([]byte(sql), 0) // null-terminated
	length := uint32(4 + len(payload))

	buf := make([]byte, 1+4+len(payload))
	buf[0] = MsgQuery
	binary.BigEndian.PutUint32(buf[1:5], length)
	copy(buf[5:], payload)

	p.Append(buf)

	msg, err := p.ParseMessage()
	if err != nil {
		t.Fatalf("ParseMessage failed: %v", err)
	}

	if msg.Type != MsgQuery {
		t.Errorf("expected type 'Q', got '%c'", msg.Type)
	}

	if msg.Length != length {
		t.Errorf("expected length %d, got %d", length, msg.Length)
	}

	parsedSQL := ParseQueryMessage(msg.Payload)
	if parsedSQL != sql {
		t.Errorf("expected SQL %q, got %q", sql, parsedSQL)
	}
}

func TestParseMessage_Incomplete(t *testing.T) {
	p := NewParser()

	// Only partial header
	p.Append([]byte{'Q', 0, 0})

	_, err := p.ParseMessage()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("expected ErrIncomplete, got %v", err)
	}
}

func TestParseMessage_MultipleMessages(t *testing.T) {
	p := NewParser()

	// Build two messages
	for _, sql := range []string{"SELECT 1", "SELECT 2"} {
		payload := append([]byte(sql), 0)
		length := uint32(4 + len(payload))
		buf := make([]byte, 1+4+len(payload))
		buf[0] = MsgQuery
		binary.BigEndian.PutUint32(buf[1:5], length)
		copy(buf[5:], payload)
		p.Append(buf)
	}

	for _, expected := range []string{"SELECT 1", "SELECT 2"} {
		msg, err := p.ParseMessage()
		if err != nil {
			t.Fatalf("ParseMessage failed: %v", err)
		}
		parsedSQL := ParseQueryMessage(msg.Payload)
		if parsedSQL != expected {
			t.Errorf("expected %q, got %q", expected, parsedSQL)
		}
	}

	// Should be incomplete now
	_, err := p.ParseMessage()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("expected ErrIncomplete after consuming all messages")
	}
}

func TestParseStartupMessage(t *testing.T) {
	p := NewParser()

	// Build a StartupMessage: length(4) + version(4) + "user\0postgres\0\0"
	params := "user\x00postgres\x00database\x00testdb\x00\x00"
	payloadLen := 4 + len(params) // version + params
	length := uint32(4 + payloadLen)

	buf := make([]byte, length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	binary.BigEndian.PutUint32(buf[4:8], ProtocolVersion30) // version 3.0
	copy(buf[8:], params)

	p.Append(buf)

	msg, err := p.ParseStartupMessage()
	if err != nil {
		t.Fatalf("ParseStartupMessage failed: %v", err)
	}

	if msg.Type != 0 {
		t.Errorf("expected type 0 (untyped), got %d", msg.Type)
	}

	version, parsedParams := ParseStartupPayload(msg.Payload)
	if version != ProtocolVersion30 {
		t.Errorf("expected version %d, got %d", ProtocolVersion30, version)
	}

	if parsedParams["user"] != "postgres" {
		t.Errorf("expected user 'postgres', got %q", parsedParams["user"])
	}

	if parsedParams["database"] != "testdb" {
		t.Errorf("expected database 'testdb', got %q", parsedParams["database"])
	}
}

func TestSSLRequest(t *testing.T) {
	p := NewParser()

	// SSLRequest: length=8, code=80877103
	buf := []byte{0x00, 0x00, 0x00, 0x08, 0x04, 0xD2, 0x16, 0x2F}
	p.Append(buf)

	msg, err := p.ParseStartupMessage()
	if err != nil {
		t.Fatalf("ParseStartupMessage failed: %v", err)
	}

	if !IsSSLRequest(msg.Payload) {
		t.Error("expected SSLRequest")
	}
}

func TestParseParseMessage(t *testing.T) {
	// Parse message payload: statement_name\0 + query_text\0 + num_params(2) + ...
	payload := []byte("mystmt\x00SELECT * FROM users WHERE id = $1\x00\x00\x01\x00\x00\x00\x17")

	stmtName, sql := ParseParseMessage(payload)
	if stmtName != "mystmt" {
		t.Errorf("expected stmt 'mystmt', got %q", stmtName)
	}
	if sql != "SELECT * FROM users WHERE id = $1" {
		t.Errorf("expected SQL, got %q", sql)
	}
}

func TestParseBindMessage(t *testing.T) {
	// Bind message payload: portal\0 + statement\0 + ...
	payload := []byte("\x00mystmt\x00\x00\x01\x00\x01\x00\x00\x00\x0242\x00\x01\x00\x00")

	portal, stmtName := ParseBindMessage(payload)
	if portal != "" {
		t.Errorf("expected empty portal, got %q", portal)
	}
	if stmtName != "mystmt" {
		t.Errorf("expected stmt 'mystmt', got %q", stmtName)
	}
}

func TestParseCommandComplete(t *testing.T) {
	payload := []byte("SELECT 5\x00")
	tag := ParseCommandComplete(payload)
	if tag != "SELECT 5" {
		t.Errorf("expected 'SELECT 5', got %q", tag)
	}
}

func TestParseErrorResponse(t *testing.T) {
	// ErrorResponse payload: field_type + value\0 ... + \0
	payload := []byte("SERROR\x00C42P01\x00Mrelation \"foo\" does not exist\x00\x00")
	severity, code, message := ParseErrorResponse(payload)

	if severity != "ERROR" {
		t.Errorf("expected severity 'ERROR', got %q", severity)
	}
	if code != "42P01" {
		t.Errorf("expected code '42P01', got %q", code)
	}
	if message != "relation \"foo\" does not exist" {
		t.Errorf("expected message about relation, got %q", message)
	}
}

func TestParseRowCount(t *testing.T) {
	tests := []struct {
		tag      string
		expected int64
	}{
		{"SELECT 5", 5},
		{"INSERT 0 3", 3},
		{"UPDATE 2", 2},
		{"DELETE 1", 1},
		{"COPY 100", 100},
		{"CREATE TABLE", 0},
		{"", 0},
	}

	for _, tt := range tests {
		got := parseRowCount(tt.tag)
		if got != tt.expected {
			t.Errorf("parseRowCount(%q) = %d, want %d", tt.tag, got, tt.expected)
		}
	}
}

func TestParseRowDescription(t *testing.T) {
	// Build a RowDescription payload: 2 columns
	// field_count(2) + [name\0 + table_oid(4) + col_attr(2) + type_oid(4) + type_size(2) + type_modifier(4) + format_code(2)] * 2
	var payload []byte
	// Field count = 2
	payload = append(payload, 0, 2)

	// Column 1: "id"
	payload = append(payload, []byte("id\x00")...)
	payload = append(payload, 0, 0, 0, 0)  // table_oid
	payload = append(payload, 0, 1)        // col_attr
	payload = append(payload, 0, 0, 0, 23) // type_oid (int4 = 23)
	payload = append(payload, 0, 4)        // type_size
	payload = appendInt32(payload, -1)     // type_modifier
	payload = append(payload, 0, 0)        // format_code (text)

	// Column 2: "name"
	payload = append(payload, []byte("name\x00")...)
	payload = append(payload, 0, 0, 0, 0)  // table_oid
	payload = append(payload, 0, 2)        // col_attr
	payload = append(payload, 0, 0, 0, 25) // type_oid (text = 25)
	payload = append(payload, 0xFF, 0xFF)  // type_size (-1 = variable)
	payload = appendInt32(payload, -1)     // type_modifier
	payload = append(payload, 0, 0)        // format_code (text)

	cols, err := ParseRowDescription(payload)
	if err != nil {
		t.Fatalf("ParseRowDescription failed: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if cols[0].Name != "id" {
		t.Errorf("expected column 0 name 'id', got %q", cols[0].Name)
	}
	if cols[1].Name != "name" {
		t.Errorf("expected column 1 name 'name', got %q", cols[1].Name)
	}
	if cols[0].TypeOID != 23 {
		t.Errorf("expected type OID 23, got %d", cols[0].TypeOID)
	}
}

func appendInt32(buf []byte, v int32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(v))
	return append(buf, b...)
}

func TestParseDataRow(t *testing.T) {
	// Build a DataRow payload: 2 columns, values "1" and "alice"
	var payload []byte
	payload = append(payload, 0, 2) // column count

	// Column 1: "1" (length=1)
	binary.BigEndian.PutUint32(payload[len(payload):len(payload)+4], 0) // placeholder
	payload = append(payload, 0, 0, 0, 1)                               // length = 1
	payload = append(payload, '1')

	// Column 2: "alice" (length=5)
	payload = append(payload, 0, 0, 0, 5) // length = 5
	payload = append(payload, []byte("alice")...)

	values, nulls, truncated, err := ParseDataRow(payload)
	if err != nil {
		t.Fatalf("ParseDataRow failed: %v", err)
	}
	if truncated {
		t.Error("unexpected truncation")
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	if values[0] != "1" || values[1] != "alice" {
		t.Errorf("expected [1, alice], got %v", values)
	}
	if nulls[0] || nulls[1] {
		t.Error("unexpected nulls")
	}
}

func TestParseDataRow_NullValues(t *testing.T) {
	// 2 columns: NULL, "hello"
	var payload []byte
	payload = append(payload, 0, 2) // column count

	// Column 1: NULL (length = -1)
	payload = append(payload, 0xFF, 0xFF, 0xFF, 0xFF) // -1

	// Column 2: "hello"
	payload = append(payload, 0, 0, 0, 5)
	payload = append(payload, []byte("hello")...)

	values, nulls, _, err := ParseDataRow(payload)
	if err != nil {
		t.Fatalf("ParseDataRow failed: %v", err)
	}
	if !nulls[0] {
		t.Error("expected column 0 to be NULL")
	}
	if nulls[1] {
		t.Error("expected column 1 to not be NULL")
	}
	if values[1] != "hello" {
		t.Errorf("expected 'hello', got %q", values[1])
	}
}

func TestParseDataRow_EmptyResult(t *testing.T) {
	// 0 columns
	payload := []byte{0, 0}
	values, nulls, _, err := ParseDataRow(payload)
	if err != nil {
		t.Fatalf("ParseDataRow failed: %v", err)
	}
	if len(values) != 0 || len(nulls) != 0 {
		t.Errorf("expected empty result, got %d values", len(values))
	}
}

func TestParseSSLResponse(t *testing.T) {
	p := NewParser()
	p.Append([]byte{'S'})

	resp, err := p.ParseSSLResponse()
	if err != nil {
		t.Fatalf("ParseSSLResponse failed: %v", err)
	}
	if resp != 'S' {
		t.Errorf("expected 'S', got '%c'", resp)
	}
}
