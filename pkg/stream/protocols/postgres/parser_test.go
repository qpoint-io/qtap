package postgres

import (
	"encoding/binary"
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
	if err != ErrIncomplete {
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
	if err != ErrIncomplete {
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
