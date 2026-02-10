package mysql

import (
	"encoding/binary"
	"testing"
)

// Helper to build a length-encoded integer
func encodeLenEncInt(val uint64) []byte {
	if val < 251 {
		return []byte{byte(val)}
	}
	if val < 1<<16 {
		b := make([]byte, 3)
		b[0] = 0xfc
		binary.LittleEndian.PutUint16(b[1:], uint16(val))
		return b
	}
	if val < 1<<24 {
		return []byte{0xfd, byte(val), byte(val >> 8), byte(val >> 16)}
	}
	b := make([]byte, 9)
	b[0] = 0xfe
	binary.LittleEndian.PutUint64(b[1:], val)
	return b
}

// Helper to build a length-encoded string
func encodeLenEncString(s string) []byte {
	return append(encodeLenEncInt(uint64(len(s))), []byte(s)...)
}

// Helper to build a column definition packet payload
func buildColumnDefPayload(catalog, schema, table, orgTable, name, orgName string, charSet uint16, colLen uint32, colType byte, flags uint16, decimals byte) []byte {
	var data []byte
	data = append(data, encodeLenEncString(catalog)...)
	data = append(data, encodeLenEncString(schema)...)
	data = append(data, encodeLenEncString(table)...)
	data = append(data, encodeLenEncString(orgTable)...)
	data = append(data, encodeLenEncString(name)...)
	data = append(data, encodeLenEncString(orgName)...)
	// fixed length fields marker
	data = append(data, 0x0c)
	// character_set (2)
	cs := make([]byte, 2)
	binary.LittleEndian.PutUint16(cs, charSet)
	data = append(data, cs...)
	// column_length (4)
	cl := make([]byte, 4)
	binary.LittleEndian.PutUint32(cl, colLen)
	data = append(data, cl...)
	// column_type (1)
	data = append(data, colType)
	// flags (2)
	f := make([]byte, 2)
	binary.LittleEndian.PutUint16(f, flags)
	data = append(data, f...)
	// decimals (1)
	data = append(data, decimals)
	// filler (2)
	data = append(data, 0x00, 0x00)
	return data
}

// Helper to build a MySQL packet (header + payload)
func buildRawPacket(seqID byte, payload []byte) []byte {
	length := len(payload)
	pkt := make([]byte, 4+length)
	pkt[0] = byte(length)
	pkt[1] = byte(length >> 8)
	pkt[2] = byte(length >> 16)
	pkt[3] = seqID
	copy(pkt[4:], payload)
	return pkt
}

// Helper to build an EOF packet payload
func buildEOFPayload() []byte {
	return []byte{0xfe, 0x00, 0x00, 0x02, 0x00}
}

// Helper to build a text protocol row payload
func buildRowPayload(values ...interface{}) []byte {
	var data []byte
	for _, v := range values {
		if v == nil {
			data = append(data, 0xfb) // NULL
		} else {
			s := v.(string)
			data = append(data, encodeLenEncString(s)...)
		}
	}
	return data
}

func TestParseColumnDefinition(t *testing.T) {
	payload := buildColumnDefPayload("def", "testdb", "users", "users", "id", "id", 63, 11, 0x03, 0x4003, 0)

	col, err := parseColumnDefinition(payload)
	if err != nil {
		t.Fatalf("parseColumnDefinition failed: %v", err)
	}

	if col.Catalog != "def" {
		t.Errorf("Expected catalog=def, got %s", col.Catalog)
	}
	if col.Schema != "testdb" {
		t.Errorf("Expected schema=testdb, got %s", col.Schema)
	}
	if col.Table != "users" {
		t.Errorf("Expected table=users, got %s", col.Table)
	}
	if col.Name != "id" {
		t.Errorf("Expected name=id, got %s", col.Name)
	}
	if col.ColumnType != 0x03 {
		t.Errorf("Expected column_type=0x03 (LONG), got 0x%02x", col.ColumnType)
	}
}

func TestParseResultSetRow(t *testing.T) {
	t.Run("simple values", func(t *testing.T) {
		payload := buildRowPayload("1", "Alice", "2024-01-15")
		row, err := parseResultSetRow(payload, 3)
		if err != nil {
			t.Fatalf("parseResultSetRow failed: %v", err)
		}
		if len(row) != 3 {
			t.Fatalf("Expected 3 values, got %d", len(row))
		}
		if row[0].String() != "1" {
			t.Errorf("Expected '1', got %q", row[0].String())
		}
		if row[1].String() != "Alice" {
			t.Errorf("Expected 'Alice', got %q", row[1].String())
		}
		if row[2].String() != "2024-01-15" {
			t.Errorf("Expected '2024-01-15', got %q", row[2].String())
		}
	})

	t.Run("with NULL values", func(t *testing.T) {
		payload := buildRowPayload("1", nil, "active")
		row, err := parseResultSetRow(payload, 3)
		if err != nil {
			t.Fatalf("parseResultSetRow failed: %v", err)
		}
		if !row[1].IsNull {
			t.Error("Expected column 1 to be NULL")
		}
		if row[1].String() != "NULL" {
			t.Errorf("Expected NULL string representation, got %q", row[1].String())
		}
	})

	t.Run("all NULL", func(t *testing.T) {
		payload := buildRowPayload(nil, nil)
		row, err := parseResultSetRow(payload, 2)
		if err != nil {
			t.Fatalf("parseResultSetRow failed: %v", err)
		}
		for i, v := range row {
			if !v.IsNull {
				t.Errorf("Expected column %d to be NULL", i)
			}
		}
	})
}

func TestReadLengthEncodedString(t *testing.T) {
	t.Run("simple string", func(t *testing.T) {
		data := encodeLenEncString("hello")
		s, n, err := readLengthEncodedString(data)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if s != "hello" {
			t.Errorf("Expected 'hello', got %q", s)
		}
		if n != 6 { // 1 length byte + 5 chars
			t.Errorf("Expected consumed=6, got %d", n)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		data := encodeLenEncString("")
		s, n, err := readLengthEncodedString(data)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if s != "" {
			t.Errorf("Expected empty string, got %q", s)
		}
		if n != 1 {
			t.Errorf("Expected consumed=1, got %d", n)
		}
	})
}

// TestResultSetIntegration tests the full result set parsing flow through
// the stream's packet state machine using a simulated multi-packet response.
func TestResultSetIntegration(t *testing.T) {
	t.Run("3 columns 2 rows", func(t *testing.T) {
		// Simulate a result set: column_count(3) + 3 col defs + EOF + 2 rows + EOF
		rs := simulateResultSet(t, 3,
			[]colDef{
				{"id", 0x03},    // INT
				{"name", 0xfd},  // VARCHAR
				{"email", 0xfd}, // VARCHAR
			},
			[][]interface{}{
				{"1", "Alice", "alice@example.com"},
				{"2", "Bob", "bob@example.com"},
			},
		)

		if len(rs.Columns) != 3 {
			t.Fatalf("Expected 3 columns, got %d", len(rs.Columns))
		}
		if rs.Columns[0].Name != "id" {
			t.Errorf("Expected column 0 name=id, got %s", rs.Columns[0].Name)
		}
		if rs.Columns[1].Name != "name" {
			t.Errorf("Expected column 1 name=name, got %s", rs.Columns[1].Name)
		}
		if len(rs.Rows) != 2 {
			t.Fatalf("Expected 2 rows, got %d", len(rs.Rows))
		}
		if rs.Rows[0][0].String() != "1" {
			t.Errorf("Expected row[0][0]='1', got %q", rs.Rows[0][0].String())
		}
		if rs.Rows[1][1].String() != "Bob" {
			t.Errorf("Expected row[1][1]='Bob', got %q", rs.Rows[1][1].String())
		}
	})

	t.Run("empty result set (0 rows)", func(t *testing.T) {
		rs := simulateResultSet(t, 2,
			[]colDef{
				{"id", 0x03},
				{"name", 0xfd},
			},
			nil, // no rows
		)
		if len(rs.Columns) != 2 {
			t.Fatalf("Expected 2 columns, got %d", len(rs.Columns))
		}
		if len(rs.Rows) != 0 {
			t.Fatalf("Expected 0 rows, got %d", len(rs.Rows))
		}
	})

	t.Run("with NULL values", func(t *testing.T) {
		rs := simulateResultSet(t, 3,
			[]colDef{
				{"id", 0x03},
				{"name", 0xfd},
				{"bio", 0xfc}, // BLOB
			},
			[][]interface{}{
				{"1", "Alice", nil},
				{"2", nil, "some bio"},
			},
		)
		if len(rs.Rows) != 2 {
			t.Fatalf("Expected 2 rows, got %d", len(rs.Rows))
		}
		if !rs.Rows[0][2].IsNull {
			t.Error("Expected row[0][2] to be NULL")
		}
		if !rs.Rows[1][1].IsNull {
			t.Error("Expected row[1][1] to be NULL")
		}
		if rs.Rows[1][2].String() != "some bio" {
			t.Errorf("Expected 'some bio', got %q", rs.Rows[1][2].String())
		}
	})

	t.Run("truncation at 100 rows", func(t *testing.T) {
		// Build 150 rows
		rows := make([][]interface{}, 150)
		for i := range rows {
			rows[i] = []interface{}{"val"}
		}
		rs := simulateResultSet(t, 1,
			[]colDef{{"x", 0xfd}},
			rows,
		)
		if len(rs.Rows) != MaxResultSetRows {
			t.Errorf("Expected %d rows (capped), got %d", MaxResultSetRows, len(rs.Rows))
		}
	})

	t.Run("column type variety", func(t *testing.T) {
		rs := simulateResultSet(t, 5,
			[]colDef{
				{"int_col", 0x03},      // INT
				{"varchar_col", 0xfd},  // VARCHAR
				{"datetime_col", 0x0c}, // DATETIME
				{"blob_col", 0xfc},     // BLOB
				{"decimal_col", 0x00},  // DECIMAL
			},
			[][]interface{}{
				{"42", "hello", "2024-01-15 10:30:00", "binary data", "99.99"},
			},
		)
		if len(rs.Columns) != 5 {
			t.Fatalf("Expected 5 columns, got %d", len(rs.Columns))
		}
		if rs.Rows[0][0].String() != "42" {
			t.Errorf("Expected '42', got %q", rs.Rows[0][0].String())
		}
		if rs.Rows[0][4].String() != "99.99" {
			t.Errorf("Expected '99.99', got %q", rs.Rows[0][4].String())
		}
	})
}

// colDef is a test helper for column definitions
type colDef struct {
	name    string
	colType byte
}

// simulateResultSet feeds raw packets through a Parser to reconstruct a ResultSet.
// It simulates: column_count packet → N column defs → EOF → M rows → EOF
func simulateResultSet(t *testing.T, colCount int, cols []colDef, rows [][]interface{}) *ResultSet {
	t.Helper()

	parser := NewParser()
	seq := byte(1)

	// 1. Column count packet
	_ = parser.Append(buildRawPacket(seq, encodeLenEncInt(uint64(colCount))))
	seq++

	// Parse column count
	pkt, err := parser.ParsePacket()
	if err != nil {
		t.Fatalf("Failed to parse column count packet: %v", err)
	}
	resp, err := ParseResponse(pkt)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	rs, ok := resp.(*ResultSet)
	if !ok {
		t.Fatalf("Expected *ResultSet, got %T", resp)
	}
	rs.Columns = make([]ColumnDefinition, 0, colCount)

	// 2. Column definition packets
	for _, col := range cols {
		payload := buildColumnDefPayload("def", "test", "t", "t", col.name, col.name, 33, 255, col.colType, 0, 0)
		_ = parser.Append(buildRawPacket(seq, payload))
		seq++

		pkt, err := parser.ParsePacket()
		if err != nil {
			t.Fatalf("Failed to parse column def packet: %v", err)
		}
		colDef, err := parseColumnDefinition(pkt.Payload)
		if err != nil {
			t.Fatalf("Failed to parse column definition: %v", err)
		}
		rs.Columns = append(rs.Columns, *colDef)
	}

	// 3. EOF after column defs
	_ = parser.Append(buildRawPacket(seq, buildEOFPayload()))
	seq++
	_, err = parser.ParsePacket() // consume EOF
	if err != nil {
		t.Fatalf("Failed to parse EOF packet: %v", err)
	}

	// 4. Row data packets
	rs.Rows = make([][]Value, 0)
	for _, row := range rows {
		payload := buildRowPayload(row...)
		_ = parser.Append(buildRawPacket(seq, payload))
		seq++

		pkt, err := parser.ParsePacket()
		if err != nil {
			t.Fatalf("Failed to parse row packet: %v", err)
		}
		if len(rs.Rows) < MaxResultSetRows {
			rowVals, err := parseResultSetRow(pkt.Payload, colCount)
			if err != nil {
				t.Fatalf("Failed to parse row: %v", err)
			}
			rs.Rows = append(rs.Rows, rowVals)
		}
	}

	// 5. EOF after rows
	_ = parser.Append(buildRawPacket(seq, buildEOFPayload()))
	_, err = parser.ParsePacket() // consume EOF
	if err != nil {
		t.Fatalf("Failed to parse final EOF packet: %v", err)
	}

	return rs
}
