package postgres

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"go.uber.org/zap"
)

// testStream creates a Stream in stateReady with no plugin manager (for unit tests)
func testStream() *Stream {
	logger := zap.NewNop()
	conn := &connection.Connection{
		OpenEvent: &connection.OpenEvent{
			Source: connection.Client,
		},
	}
	s := NewStream(context.Background(), logger, conn)
	s.state = stateReady
	return s
}

// buildTypedMessage builds a typed PostgreSQL message (type byte + length + payload)
func buildTypedMessage(msgType byte, payload []byte) []byte {
	length := uint32(4 + len(payload))
	buf := make([]byte, 1+4+len(payload))
	buf[0] = msgType
	binary.BigEndian.PutUint32(buf[1:5], length)
	copy(buf[5:], payload)
	return buf
}

// buildParsePayload builds a Parse message payload: stmtName\0 + sql\0 + numParams(0)
func buildParsePayload(stmtName, sql string) []byte {
	var buf []byte
	buf = append(buf, []byte(stmtName)...)
	buf = append(buf, 0)
	buf = append(buf, []byte(sql)...)
	buf = append(buf, 0)
	// Number of parameter types (int16) = 0
	buf = append(buf, 0, 0)
	return buf
}

// buildBindPayload builds a Bind message payload: portal\0 + stmtName\0 + format codes + params + result formats
func buildBindPayload(portal, stmtName string) []byte {
	var buf []byte
	buf = append(buf, []byte(portal)...)
	buf = append(buf, 0)
	buf = append(buf, []byte(stmtName)...)
	buf = append(buf, 0)
	// Number of format codes (int16) = 0
	buf = append(buf, 0, 0)
	// Number of parameters (int16) = 0
	buf = append(buf, 0, 0)
	// Number of result format codes (int16) = 0
	buf = append(buf, 0, 0)
	return buf
}

// buildExecutePayload builds an Execute message payload: portal\0 + maxRows(int32)
func buildExecutePayload(portal string, maxRows int32) []byte {
	var buf []byte
	buf = append(buf, []byte(portal)...)
	buf = append(buf, 0)
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(maxRows))
	buf = append(buf, b...)
	return buf
}

// buildSyncMessage builds a Sync message (no payload)
func buildSyncMessage() []byte {
	return buildTypedMessage(MsgSync, nil)
}

// buildClosePayload builds a Close message payload: type('S' or 'P') + name\0
func buildClosePayload(closeType byte, name string) []byte {
	var buf []byte
	buf = append(buf, closeType)
	buf = append(buf, []byte(name)...)
	buf = append(buf, 0)
	return buf
}

// buildCommandCompletePayload builds a CommandComplete payload: tag\0
func buildCommandCompletePayload(tag string) []byte {
	return append([]byte(tag), 0)
}

// buildReadyForQueryPayload builds a ReadyForQuery payload: txStatus byte
func buildReadyForQueryPayload(txStatus byte) []byte {
	return []byte{txStatus}
}

// sendRequest sends client→server data through the stream
func sendRequest(s *Stream, data []byte) {
	event := &connection.DataEvent{
		Direction: connection.Egress,
		Data:      data,
	}
	s.Process(event)
}

// sendResponse sends server→client data through the stream
func sendResponse(s *Stream, data []byte) {
	event := &connection.DataEvent{
		Direction: connection.Ingress,
		Data:      data,
	}
	s.Process(event)
}

// TestUnnamedPreparedStatement tests the basic unnamed prepared statement flow
// Parse("", sql) → Bind("", "") → Execute("") → Sync
func TestUnnamedPreparedStatement(t *testing.T) {
	s := testStream()

	sql := "SELECT * FROM users WHERE id = $1"

	// Parse (unnamed)
	parsePayload := buildParsePayload("", sql)
	sendRequest(s, buildTypedMessage(MsgParse, parsePayload))

	// Bind (unnamed portal, unnamed statement)
	bindPayload := buildBindPayload("", "")
	sendRequest(s, buildTypedMessage(MsgBind, bindPayload))

	// Execute
	execPayload := buildExecutePayload("", 0)
	sendRequest(s, buildTypedMessage(MsgExecute, execPayload))

	// Sync
	sendRequest(s, buildSyncMessage())

	// Verify: one pending command with the correct SQL
	if len(s.pendingCommands) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(s.pendingCommands))
	}
	if s.pendingCommands[0].Query != sql {
		t.Errorf("expected SQL %q, got %q", sql, s.pendingCommands[0].Query)
	}
}

// TestNamedPreparedStatement tests Parse once, Execute multiple times
func TestNamedPreparedStatement(t *testing.T) {
	s := testStream()

	sql := "SELECT * FROM users WHERE id = $1"

	// Parse with name "stmt1"
	parsePayload := buildParsePayload("stmt1", sql)
	sendRequest(s, buildTypedMessage(MsgParse, parsePayload))

	// First execution: Bind("", "stmt1") → Execute
	bindPayload := buildBindPayload("", "stmt1")
	sendRequest(s, buildTypedMessage(MsgBind, bindPayload))
	execPayload := buildExecutePayload("", 0)
	sendRequest(s, buildTypedMessage(MsgExecute, execPayload))
	sendRequest(s, buildSyncMessage())

	// Second execution: Bind("", "stmt1") → Execute (no new Parse!)
	sendRequest(s, buildTypedMessage(MsgBind, bindPayload))
	sendRequest(s, buildTypedMessage(MsgExecute, execPayload))
	sendRequest(s, buildSyncMessage())

	// Third execution
	sendRequest(s, buildTypedMessage(MsgBind, bindPayload))
	sendRequest(s, buildTypedMessage(MsgExecute, execPayload))
	sendRequest(s, buildSyncMessage())

	// Verify: three pending commands, all with the same SQL
	if len(s.pendingCommands) != 3 {
		t.Fatalf("expected 3 pending commands, got %d", len(s.pendingCommands))
	}
	for i, cmd := range s.pendingCommands {
		if cmd.Query != sql {
			t.Errorf("command %d: expected SQL %q, got %q", i, sql, cmd.Query)
		}
	}
}

// TestMultipleNamedStatements tests having multiple named statements active simultaneously
func TestMultipleNamedStatements(t *testing.T) {
	s := testStream()

	sql1 := "SELECT * FROM users WHERE id = $1"
	sql2 := "INSERT INTO logs (msg) VALUES ($1)"
	sql3 := "UPDATE users SET name = $1 WHERE id = $2"

	// Parse all three statements
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("get_user", sql1)))
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("add_log", sql2)))
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("update_user", sql3)))

	// Execute them in a different order
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "add_log")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "get_user")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "update_user")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	sendRequest(s, buildSyncMessage())

	// Verify: three pending commands with correct SQL in execution order
	if len(s.pendingCommands) != 3 {
		t.Fatalf("expected 3 pending commands, got %d", len(s.pendingCommands))
	}
	expected := []string{sql2, sql1, sql3}
	for i, cmd := range s.pendingCommands {
		if cmd.Query != expected[i] {
			t.Errorf("command %d: expected SQL %q, got %q", i, expected[i], cmd.Query)
		}
	}
}

// TestCloseStatementCleanup tests that Close('S', name) removes the prepared statement
func TestCloseStatementCleanup(t *testing.T) {
	s := testStream()

	sql := "SELECT 1"

	// Parse named statement
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("temp_stmt", sql)))

	// Verify it's stored
	if _, ok := s.preparedStatements["temp_stmt"]; !ok {
		t.Fatal("expected 'temp_stmt' in preparedStatements")
	}

	// Close the statement
	closePayload := buildClosePayload('S', "temp_stmt")
	sendRequest(s, buildTypedMessage(MsgClose, closePayload))

	// Verify it's removed
	if _, ok := s.preparedStatements["temp_stmt"]; ok {
		t.Fatal("expected 'temp_stmt' to be removed from preparedStatements after Close")
	}

	// Binding to the closed statement should result in empty SQL
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "temp_stmt")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	// No command should be enqueued (SQL is empty)
	if len(s.pendingCommands) != 0 {
		t.Fatalf("expected 0 pending commands after binding to closed statement, got %d", len(s.pendingCommands))
	}
}

// TestClosePortalDoesNotAffectStatements ensures Close('P', ...) doesn't remove statements
func TestClosePortalDoesNotAffectStatements(t *testing.T) {
	s := testStream()

	sql := "SELECT 1"
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("my_stmt", sql)))

	// Close a portal (not a statement)
	closePayload := buildClosePayload('P', "my_portal")
	sendRequest(s, buildTypedMessage(MsgClose, closePayload))

	// Statement should still exist
	if _, ok := s.preparedStatements["my_stmt"]; !ok {
		t.Fatal("expected 'my_stmt' to still exist after closing a portal")
	}
}

// TestMixedNamedAndUnnamed tests interleaving named and unnamed statements
func TestMixedNamedAndUnnamed(t *testing.T) {
	s := testStream()

	namedSQL := "SELECT * FROM users WHERE id = $1"
	unnamedSQL := "SELECT count(*) FROM users"

	// Parse named statement
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("user_query", namedSQL)))

	// Execute unnamed inline
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("", unnamedSQL)))
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	// Execute named
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "user_query")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	// Another unnamed
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("", "SELECT 1")))
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	sendRequest(s, buildSyncMessage())

	if len(s.pendingCommands) != 3 {
		t.Fatalf("expected 3 pending commands, got %d", len(s.pendingCommands))
	}

	expected := []string{unnamedSQL, namedSQL, "SELECT 1"}
	for i, cmd := range s.pendingCommands {
		if cmd.Query != expected[i] {
			t.Errorf("command %d: expected SQL %q, got %q", i, expected[i], cmd.Query)
		}
	}
}

// TestPipelinedBindExecute tests multiple Bind+Execute in a single pipeline (before Sync)
func TestPipelinedBindExecute(t *testing.T) {
	s := testStream()

	sql1 := "SELECT * FROM users"
	sql2 := "SELECT * FROM orders"

	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("q1", sql1)))
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("q2", sql2)))

	// Pipeline: Bind+Execute for both, then one Sync
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "q1")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "q2")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))
	sendRequest(s, buildSyncMessage())

	if len(s.pendingCommands) != 2 {
		t.Fatalf("expected 2 pending commands, got %d", len(s.pendingCommands))
	}

	if s.pendingCommands[0].Query != sql1 {
		t.Errorf("command 0: expected %q, got %q", sql1, s.pendingCommands[0].Query)
	}
	if s.pendingCommands[1].Query != sql2 {
		t.Errorf("command 1: expected %q, got %q", sql2, s.pendingCommands[1].Query)
	}
}

// TestCommandCorrelation tests full request→response correlation
func TestCommandCorrelation(t *testing.T) {
	s := testStream()

	sql := "INSERT INTO users (name) VALUES ($1)"

	// Send Parse → Bind → Execute → Sync
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("ins", sql)))
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "ins")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))
	sendRequest(s, buildSyncMessage())

	if len(s.pendingCommands) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(s.pendingCommands))
	}

	// Server responds: ParseComplete, BindComplete, CommandComplete, ReadyForQuery
	sendResponse(s, buildTypedMessage(MsgParseComplete, nil))
	sendResponse(s, buildTypedMessage(MsgBindComplete, nil))
	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("INSERT 0 1")))
	sendResponse(s, buildTypedMessage(MsgReadyForQuery, buildReadyForQueryPayload(TxStatusIdle)))

	// Pending commands should be drained
	if len(s.pendingCommands) != 0 {
		t.Fatalf("expected 0 pending commands after response, got %d", len(s.pendingCommands))
	}
}

// TestParseCloseMessage tests the parser helper
func TestParseCloseMessage(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		wantType byte
		wantName string
	}{
		{
			name:     "close statement",
			payload:  append([]byte{'S'}, append([]byte("mystmt"), 0)...),
			wantType: 'S',
			wantName: "mystmt",
		},
		{
			name:     "close portal",
			payload:  append([]byte{'P'}, append([]byte("myportal"), 0)...),
			wantType: 'P',
			wantName: "myportal",
		},
		{
			name:     "close unnamed statement",
			payload:  []byte{'S', 0},
			wantType: 'S',
			wantName: "",
		},
		{
			name:     "empty payload",
			payload:  nil,
			wantType: 0,
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotName := ParseCloseMessage(tt.payload)
			if gotType != tt.wantType {
				t.Errorf("closeType = %c, want %c", gotType, tt.wantType)
			}
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

// TestSimpleQueryStillWorks ensures Simple Query Protocol still works (regression test)
func TestSimpleQueryStillWorks(t *testing.T) {
	s := testStream()

	sql := "SELECT * FROM pg_stat_activity"
	queryPayload := append([]byte(sql), 0)
	sendRequest(s, buildTypedMessage(MsgQuery, queryPayload))

	if len(s.pendingCommands) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(s.pendingCommands))
	}
	if s.pendingCommands[0].Query != sql {
		t.Errorf("expected %q, got %q", sql, s.pendingCommands[0].Query)
	}

	// Response
	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("SELECT 5")))
	sendResponse(s, buildTypedMessage(MsgReadyForQuery, buildReadyForQueryPayload(TxStatusIdle)))

	if len(s.pendingCommands) != 0 {
		t.Fatalf("expected 0 pending after response, got %d", len(s.pendingCommands))
	}
}

// TestReParseNamedStatement tests that re-parsing a named statement updates the SQL
func TestReParseNamedStatement(t *testing.T) {
	s := testStream()

	// First parse
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("s1", "SELECT 1")))

	// Re-parse same name with different SQL
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("s1", "SELECT 2")))

	// Execute
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "s1")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	if len(s.pendingCommands) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(s.pendingCommands))
	}
	if s.pendingCommands[0].Query != "SELECT 2" {
		t.Errorf("expected updated SQL 'SELECT 2', got %q", s.pendingCommands[0].Query)
	}
}

// buildRowDescriptionPayload builds a RowDescription payload with the given column names
func buildRowDescriptionPayload(names ...string) []byte {
	var buf []byte
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(len(names)))
	buf = append(buf, b...)

	for _, name := range names {
		buf = append(buf, []byte(name)...)
		buf = append(buf, 0)
		// table_oid(4) + col_attr(2) + type_oid(4) + type_size(2) + type_modifier(4) + format_code(2) = 18 bytes
		buf = append(buf, make([]byte, 18)...)
	}
	return buf
}

// buildDataRowPayload builds a DataRow payload. nil entries represent NULL.
func buildDataRowPayload(values ...*string) []byte {
	var buf []byte
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(len(values)))
	buf = append(buf, b...)

	for _, v := range values {
		lb := make([]byte, 4)
		if v == nil {
			binary.BigEndian.PutUint32(lb, 0xFFFFFFFF) // -1 = NULL
			buf = append(buf, lb...)
		} else {
			binary.BigEndian.PutUint32(lb, uint32(len(*v)))
			buf = append(buf, lb...)
			buf = append(buf, []byte(*v)...)
		}
	}
	return buf
}

func strPtr(s string) *string { return &s }

func TestSimpleQueryWithRows(t *testing.T) {
	s := testStream()

	// Send a simple query
	sql := "SELECT id, name FROM users"
	sendRequest(s, buildTypedMessage(MsgQuery, append([]byte(sql), 0)))

	if len(s.pendingCommands) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(s.pendingCommands))
	}

	// Server sends RowDescription
	sendResponse(s, buildTypedMessage(MsgRowDescription, buildRowDescriptionPayload("id", "name")))

	// Server sends DataRows
	sendResponse(s, buildTypedMessage(MsgDataRow, buildDataRowPayload(strPtr("1"), strPtr("alice"))))
	sendResponse(s, buildTypedMessage(MsgDataRow, buildDataRowPayload(strPtr("2"), strPtr("bob"))))

	// Verify rows accumulated on pending command
	pending := s.pendingCommands[0]
	if len(pending.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(pending.Columns))
	}
	if pending.Columns[0].Name != "id" || pending.Columns[1].Name != "name" {
		t.Errorf("unexpected column names: %v", pending.Columns)
	}
	if len(pending.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(pending.Rows))
	}
	if pending.Rows[0][0] != "1" || pending.Rows[0][1] != "alice" {
		t.Errorf("unexpected row 0: %v", pending.Rows[0])
	}
	if pending.Rows[1][0] != "2" || pending.Rows[1][1] != "bob" {
		t.Errorf("unexpected row 1: %v", pending.Rows[1])
	}

	// CommandComplete clears it
	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("SELECT 2")))
	if len(s.pendingCommands) != 0 {
		t.Errorf("expected 0 pending commands after completion")
	}
}

func TestRowsWithNullValues(t *testing.T) {
	s := testStream()

	sendRequest(s, buildTypedMessage(MsgQuery, append([]byte("SELECT a, b"), 0)))
	sendResponse(s, buildTypedMessage(MsgRowDescription, buildRowDescriptionPayload("a", "b")))
	sendResponse(s, buildTypedMessage(MsgDataRow, buildDataRowPayload(nil, strPtr("val"))))

	pending := s.pendingCommands[0]
	if len(pending.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(pending.Rows))
	}
	if !pending.NullFlags[0][0] {
		t.Error("expected column 0 to be NULL")
	}
	if pending.NullFlags[0][1] {
		t.Error("expected column 1 to not be NULL")
	}
	if pending.Rows[0][1] != "val" {
		t.Errorf("expected 'val', got %q", pending.Rows[0][1])
	}

	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("SELECT 1")))
}

func TestEmptyResultSet(t *testing.T) {
	s := testStream()

	sendRequest(s, buildTypedMessage(MsgQuery, append([]byte("SELECT id FROM empty"), 0)))
	sendResponse(s, buildTypedMessage(MsgRowDescription, buildRowDescriptionPayload("id")))
	// No DataRow messages — straight to CommandComplete
	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("SELECT 0")))

	if len(s.pendingCommands) != 0 {
		t.Errorf("expected 0 pending commands")
	}
}

func TestRowTruncationAtMaxRows(t *testing.T) {
	s := testStream()

	sendRequest(s, buildTypedMessage(MsgQuery, append([]byte("SELECT x"), 0)))
	sendResponse(s, buildTypedMessage(MsgRowDescription, buildRowDescriptionPayload("x")))

	// Send MaxRows + 10 rows
	for range MaxRows + 10 {
		v := "val"
		sendResponse(s, buildTypedMessage(MsgDataRow, buildDataRowPayload(strPtr(v))))
	}

	pending := s.pendingCommands[0]
	if len(pending.Rows) != MaxRows {
		t.Errorf("expected %d rows, got %d", MaxRows, len(pending.Rows))
	}
	if !pending.Truncated {
		t.Error("expected Truncated to be true")
	}

	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("SELECT 110")))
}

func TestExtendedQueryWithRows(t *testing.T) {
	s := testStream()

	sql := "SELECT id FROM items"

	// Parse → Bind → Execute
	sendRequest(s, buildTypedMessage(MsgParse, buildParsePayload("", sql)))
	sendRequest(s, buildTypedMessage(MsgBind, buildBindPayload("", "")))
	sendRequest(s, buildTypedMessage(MsgExecute, buildExecutePayload("", 0)))

	if len(s.pendingCommands) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(s.pendingCommands))
	}

	// Server sends RowDescription + DataRow + CommandComplete
	sendResponse(s, buildTypedMessage(MsgRowDescription, buildRowDescriptionPayload("id")))
	sendResponse(s, buildTypedMessage(MsgDataRow, buildDataRowPayload(strPtr("42"))))
	sendResponse(s, buildTypedMessage(MsgCommandComplete, buildCommandCompletePayload("SELECT 1")))

	if len(s.pendingCommands) != 0 {
		t.Errorf("expected 0 pending commands after completion")
	}
}
