//go:build e2e

package mysql

import (
	"testing"
)

func TestE2ERealPacketParsing(t *testing.T) {
	// Test packets captured from real MySQL 5.7 traffic
	testCases := []struct {
		name        string
		hex         string
		wantCmd     byte
		wantPayload string
	}{
		{
			name:        "SELECT version_comment",
			hex:         "210000000373656c65637420404076657273696f6e5f636f6d6d656e74206c696d69742031",
			wantCmd:     ComQuery,
			wantPayload: "select @@version_comment limit 1",
		},
		{
			name:        "SELECT hello mysql parser",
			hex:         "1c0000000353454c454354202768656c6c6f206d7973716c2070617273657227",
			wantCmd:     ComQuery,
			wantPayload: "SELECT 'hello mysql parser'",
		},
		{
			name:        "SELECT VERSION()",
			hex:         "110000000353454c4543542056455253494f4e2829",
			wantCmd:     ComQuery,
			wantPayload: "SELECT VERSION()",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hexDecode(tc.hex)
			if err != nil {
				t.Fatalf("hex decode: %v", err)
			}

			p := NewParser()
			p.Append(data)

			pkt, err := p.ParsePacket()
			if err != nil {
				t.Fatalf("parse packet: %v", err)
			}

			cmd, parsed, err := ParseCommand(pkt)
			if err != nil {
				t.Fatalf("parse command: %v", err)
			}

			if cmd != tc.wantCmd {
				t.Errorf("command = %v, want %v", cmd, tc.wantCmd)
			}

			if qc, ok := parsed.(*QueryCommand); ok {
				if qc.Query != tc.wantPayload {
					t.Errorf("query = %q, want %q", qc.Query, tc.wantPayload)
				}
			}
		})
	}
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		s = "0" + s
	}
	result := make([]byte, len(s)/2)
	for i := 0; i < len(result); i++ {
		var b byte
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			switch {
			case c >= '0' && c <= '9':
				b = b*16 + (c - '0')
			case c >= 'a' && c <= 'f':
				b = b*16 + (c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				b = b*16 + (c - 'A' + 10)
			}
		}
		result[i] = b
	}
	return result, nil
}
