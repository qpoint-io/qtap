package mysql

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Real packets captured from MySQL 5.7 via tcpdump
var realPackets = []struct {
	name     string
	hex      string
	wantCmd  byte
	wantData string
}{
	{
		name:     "version_comment query",
		hex:      "210000000373656c65637420404076657273696f6e5f636f6d6d656e74206c696d69742031",
		wantCmd:  ComQuery,
		wantData: "select @@version_comment limit 1",
	},
	{
		name:     "SELECT literal string",
		hex:      "1a0000000353454c45435420277265616c5f7061636b65745f7465737427",
		wantCmd:  ComQuery,
		wantData: "SELECT 'real_packet_test'",
	},
	{
		name:     "SELECT VERSION",
		hex:      "110000000353454c4543542056455253494f4e2829",
		wantCmd:  ComQuery,
		wantData: "SELECT VERSION()",
	},
	{
		name:     "SELECT hello parser",
		hex:      "1c0000000353454c454354202768656c6c6f206d7973716c2070617273657227",
		wantCmd:  ComQuery,
		wantData: "SELECT 'hello mysql parser'",
	},
}

func TestParseRealPackets(t *testing.T) {
	for _, tt := range realPackets {
		t.Run(tt.name, func(t *testing.T) {
			// Remove any spaces from hex string
			hexStr := strings.ReplaceAll(tt.hex, " ", "")
			data, err := hex.DecodeString(hexStr)
			if err != nil {
				t.Fatalf("Failed to decode hex: %v", err)
			}

			p := NewParser()
			_ = p.Append(data)

			pkt, err := p.ParsePacket()
			if err != nil {
				t.Fatalf("ParsePacket failed: %v", err)
			}

			cmd, cmdData, err := ParseCommand(pkt)
			if err != nil {
				t.Fatalf("ParseCommand failed: %v", err)
			}

			if cmd != tt.wantCmd {
				t.Errorf("Command: expected 0x%02x (%s), got 0x%02x (%s)",
					tt.wantCmd, CommandName(tt.wantCmd),
					cmd, CommandName(cmd))
			}

			if qc, ok := cmdData.(*QueryCommand); ok {
				if qc.Query != tt.wantData {
					t.Errorf("Query: expected %q, got %q", tt.wantData, qc.Query)
				}
			}
		})
	}
}

func TestParseRealHandshake(t *testing.T) {
	// Real MySQL 5.7.44 handshake packet (captured via tcpdump)
	// Server greeting with auth plugin mysql_native_password
	hexData := "4a0000000a352e372e3434000400000017340712794a1b6400ffff080200ffc115000000000000000000001f5b2c203e6a7d0205333f58006d7973716c5f6e61746976655f70617373776f726400"

	data, err := hex.DecodeString(hexData)
	if err != nil {
		t.Fatalf("Failed to decode hex: %v", err)
	}

	p := NewParser()
	_ = p.Append(data)

	pkt, err := p.ParsePacket()
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	hs, err := ParseServerHandshake(pkt)
	if err != nil {
		t.Fatalf("ParseServerHandshake failed: %v", err)
	}

	if hs.ProtocolVersion != 10 {
		t.Errorf("Protocol version: expected 10, got %d", hs.ProtocolVersion)
	}

	if !strings.HasPrefix(hs.ServerVersion, "5.7") {
		t.Errorf("Server version: expected 5.7.x, got %s", hs.ServerVersion)
	}

	t.Logf("Parsed real handshake: version=%s, connID=%d, charset=0x%02x",
		hs.ServerVersion, hs.ConnectionID, hs.CharacterSet)
}

// Test that we can handle a stream of real packets
func TestParseRealStream(t *testing.T) {
	// Concatenate multiple real packets
	var allData []byte
	for _, tt := range realPackets {
		hexStr := strings.ReplaceAll(tt.hex, " ", "")
		data, _ := hex.DecodeString(hexStr)
		allData = append(allData, data...)
	}

	p := NewParser()
	_ = p.Append(allData)

	count := 0
	for {
		pkt, err := p.ParsePacket()
		if errors.Is(err, ErrIncomplete) {
			break
		}
		if err != nil {
			t.Fatalf("ParsePacket failed on packet %d: %v", count, err)
		}

		cmd, _, err := ParseCommand(pkt)
		if err != nil {
			t.Fatalf("ParseCommand failed on packet %d: %v", count, err)
		}

		if cmd != ComQuery {
			t.Errorf("Packet %d: expected COM_QUERY, got %s", count, CommandName(cmd))
		}

		count++
	}

	if count != len(realPackets) {
		t.Errorf("Expected %d packets, parsed %d", len(realPackets), count)
	}

	t.Logf("Successfully parsed %d real packets from stream", count)
}
