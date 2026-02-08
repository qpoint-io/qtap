package mysql

import (
	"errors"
	"testing"
)

func TestParsePacket(t *testing.T) {
	// Build a simple COM_QUERY packet: "SELECT 1"
	query := "SELECT 1"
	pktData := BuildQueryPacket(0, query)

	p := NewParser()
	p.Append(pktData)

	pkt, err := p.ParsePacket()
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	if pkt.SequenceID != 0 {
		t.Errorf("Expected seq=0, got %d", pkt.SequenceID)
	}

	if pkt.Length != uint32(len(query)+1) { // +1 for command byte
		t.Errorf("Expected length=%d, got %d", len(query)+1, pkt.Length)
	}

	cmd, data, err := ParseCommand(pkt)
	if err != nil {
		t.Fatalf("ParseCommand failed: %v", err)
	}

	if cmd != ComQuery {
		t.Errorf("Expected COM_QUERY, got %d", cmd)
	}

	qc, ok := data.(*QueryCommand)
	if !ok {
		t.Fatalf("Expected *QueryCommand, got %T", data)
	}

	if qc.Query != query {
		t.Errorf("Expected query=%q, got %q", query, qc.Query)
	}
}

func TestParseIncomplete(t *testing.T) {
	p := NewParser()

	// Only add partial header
	p.Append([]byte{0x09, 0x00, 0x00})

	_, err := p.ParsePacket()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("Expected ErrIncomplete, got %v", err)
	}

	// Add sequence ID but no payload
	p.Append([]byte{0x00})

	_, err = p.ParsePacket()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("Expected ErrIncomplete, got %v", err)
	}
}

func TestParseMultiplePackets(t *testing.T) {
	p := NewParser()

	// Add two packets
	p.Append(BuildQueryPacket(0, "SELECT 1"))
	p.Append(BuildQueryPacket(1, "SELECT 2"))

	// Parse first
	pkt1, err := p.ParsePacket()
	if err != nil {
		t.Fatalf("First ParsePacket failed: %v", err)
	}
	if pkt1.SequenceID != 0 {
		t.Errorf("First packet seq: expected 0, got %d", pkt1.SequenceID)
	}

	// Parse second
	pkt2, err := p.ParsePacket()
	if err != nil {
		t.Fatalf("Second ParsePacket failed: %v", err)
	}
	if pkt2.SequenceID != 1 {
		t.Errorf("Second packet seq: expected 1, got %d", pkt2.SequenceID)
	}

	// No more packets
	_, err = p.ParsePacket()
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("Expected ErrIncomplete for empty buffer, got %v", err)
	}
}

func TestParseOKPacket(t *testing.T) {
	// OK packet: header=0x00, affected_rows=1, last_insert_id=0, status=0x0002, warnings=0
	payload := []byte{
		0x00,       // OK header
		0x01,       // affected_rows = 1
		0x00,       // last_insert_id = 0
		0x02, 0x00, // status flags
		0x00, 0x00, // warnings
	}

	pkt := &Packet{
		Length:     uint32(len(payload)),
		SequenceID: 1,
		Payload:    payload,
	}

	resp, err := ParseResponse(pkt)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	ok, isOK := resp.(*OKResponse)
	if !isOK {
		t.Fatalf("Expected *OKResponse, got %T", resp)
	}

	if ok.AffectedRows != 1 {
		t.Errorf("Expected AffectedRows=1, got %d", ok.AffectedRows)
	}

	if ok.LastInsertID != 0 {
		t.Errorf("Expected LastInsertID=0, got %d", ok.LastInsertID)
	}
}

func TestParseERRPacket(t *testing.T) {
	// ERR packet: header=0xff, error_code=1064, sql_state=#42000, message="You have an error..."
	payload := []byte{
		0xff,       // ERR header
		0x28, 0x04, // error code 1064 (little-endian)
		'#',                     // SQL state marker
		'4', '2', '0', '0', '0', // SQL state
		'Y', 'o', 'u', ' ', 'h', 'a', 'v', 'e', // message
	}

	pkt := &Packet{
		Length:     uint32(len(payload)),
		SequenceID: 1,
		Payload:    payload,
	}

	resp, err := ParseResponse(pkt)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	errResp, isErr := resp.(*ERRResponse)
	if !isErr {
		t.Fatalf("Expected *ERRResponse, got %T", resp)
	}

	if errResp.ErrorCode != 1064 {
		t.Errorf("Expected ErrorCode=1064, got %d", errResp.ErrorCode)
	}

	if errResp.SQLState != "42000" {
		t.Errorf("Expected SQLState=42000, got %s", errResp.SQLState)
	}

	if errResp.ErrorMessage != "You have" {
		t.Errorf("Expected message prefix, got %q", errResp.ErrorMessage)
	}
}

func TestParseServerHandshake(t *testing.T) {
	// Simplified handshake packet
	payload := []byte{
		0x0a,                            // protocol version 10
		'8', '.', '0', '.', '3', '2', 0, // server version (null-terminated)
		0x01, 0x00, 0x00, 0x00, // connection ID = 1
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', // auth plugin data part 1
		0x00,       // filler
		0xff, 0xf7, // capability flags lower (CLIENT_PROTOCOL_41, etc.)
		0x21,       // character set (utf8)
		0x02, 0x00, // status flags
		0x0f, 0xff, // capability flags upper
		0x15,                         // auth plugin data length
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // reserved
		'i', 'j', 'k', 'l', 'm', 0, // auth plugin data part 2
		'c', 'a', 'c', 'h', 'i', 'n', 'g', '_', 's', 'h', 'a', '2', '_', 'p', 'a', 's', 's', 'w', 'o', 'r', 'd', 0, // auth plugin name
	}

	pkt := &Packet{
		Length:     uint32(len(payload)),
		SequenceID: 0,
		Payload:    payload,
	}

	hs, err := ParseServerHandshake(pkt)
	if err != nil {
		t.Fatalf("ParseServerHandshake failed: %v", err)
	}

	if hs.ProtocolVersion != 10 {
		t.Errorf("Expected protocol version 10, got %d", hs.ProtocolVersion)
	}

	if hs.ServerVersion != "8.0.32" {
		t.Errorf("Expected server version 8.0.32, got %s", hs.ServerVersion)
	}

	if hs.ConnectionID != 1 {
		t.Errorf("Expected connection ID 1, got %d", hs.ConnectionID)
	}

	if hs.CharacterSet != 0x21 {
		t.Errorf("Expected character set 0x21, got 0x%02x", hs.CharacterSet)
	}
}

func TestLengthEncodedInt(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected uint64
		consumed int
	}{
		{"1-byte: 0", []byte{0x00}, 0, 1},
		{"1-byte: 250", []byte{0xfa}, 250, 1},
		{"2-byte: 251", []byte{0xfc, 0xfb, 0x00}, 251, 3},
		{"2-byte: 65535", []byte{0xfc, 0xff, 0xff}, 65535, 3},
		{"3-byte", []byte{0xfd, 0x01, 0x02, 0x03}, 0x030201, 4},
		{"8-byte", []byte{0xfe, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, 0x0807060504030201, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, n, err := readLengthEncodedInt(tt.data)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if val != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, val)
			}
			if n != tt.consumed {
				t.Errorf("Expected consumed=%d, got %d", tt.consumed, n)
			}
		})
	}
}

func TestCommandNames(t *testing.T) {
	tests := []struct {
		cmd      byte
		expected string
	}{
		{ComQuery, "COM_QUERY"},
		{ComQuit, "COM_QUIT"},
		{ComPing, "COM_PING"},
		{ComStmtPrepare, "COM_STMT_PREPARE"},
		{0x99, "UNKNOWN"},
	}

	for _, tt := range tests {
		got := CommandName(tt.cmd)
		if got != tt.expected {
			t.Errorf("CommandName(0x%02x): expected %q, got %q", tt.cmd, tt.expected, got)
		}
	}
}

func TestBuildPackets(t *testing.T) {
	// Test query packet
	query := "SELECT * FROM users"
	pktData := BuildQueryPacket(5, query)

	if len(pktData) != 4+1+len(query) {
		t.Errorf("Query packet length: expected %d, got %d", 4+1+len(query), len(pktData))
	}

	if pktData[3] != 5 {
		t.Errorf("Sequence ID: expected 5, got %d", pktData[3])
	}

	if pktData[4] != ComQuery {
		t.Errorf("Command byte: expected 0x03, got 0x%02x", pktData[4])
	}

	// Test ping packet
	pingData := BuildPingPacket(0)
	if len(pingData) != 5 { // 4 header + 1 command
		t.Errorf("Ping packet length: expected 5, got %d", len(pingData))
	}
	if pingData[4] != ComPing {
		t.Errorf("Ping command: expected 0x%02x, got 0x%02x", ComPing, pingData[4])
	}
}

func TestIsPacketType(t *testing.T) {
	okPkt := &Packet{Payload: []byte{OKPacket, 0, 0, 0, 0}}
	errPkt := &Packet{Payload: []byte{ERRPacket, 0, 0}}
	eofPkt := &Packet{Payload: []byte{EOFPacket, 0, 0, 0, 0}}

	if !IsOK(okPkt) {
		t.Error("Expected OK packet to be recognized")
	}
	if !IsError(errPkt) {
		t.Error("Expected ERR packet to be recognized")
	}
	if !IsEOF(eofPkt) {
		t.Error("Expected EOF packet to be recognized")
	}
}
