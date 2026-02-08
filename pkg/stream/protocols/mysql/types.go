package mysql

// MySQL Wire Protocol Types
// Reference: https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basic_packets.html

// Packet represents a MySQL protocol packet
type Packet struct {
	Length     uint32 // 3 bytes in wire format
	SequenceID uint8
	Payload    []byte
}

// Command types (client → server)
const (
	ComSleep            byte = 0x00
	ComQuit             byte = 0x01
	ComInitDB           byte = 0x02
	ComQuery            byte = 0x03
	ComFieldList        byte = 0x04
	ComCreateDB         byte = 0x05
	ComDropDB           byte = 0x06
	ComRefresh          byte = 0x07
	ComShutdown         byte = 0x08
	ComStatistics       byte = 0x09
	ComProcessInfo      byte = 0x0a
	ComConnect          byte = 0x0b
	ComProcessKill      byte = 0x0c
	ComDebug            byte = 0x0d
	ComPing             byte = 0x0e
	ComChangeUser       byte = 0x11
	ComStmtPrepare      byte = 0x16
	ComStmtExecute      byte = 0x17
	ComStmtSendLongData byte = 0x18
	ComStmtClose        byte = 0x19
	ComStmtReset        byte = 0x1a
	ComSetOption        byte = 0x1b
	ComStmtFetch        byte = 0x1c
)

// CommandName returns a human-readable name for a command byte
func CommandName(cmd byte) string {
	switch cmd {
	case ComSleep:
		return "COM_SLEEP"
	case ComQuit:
		return "COM_QUIT"
	case ComInitDB:
		return "COM_INIT_DB"
	case ComQuery:
		return "COM_QUERY"
	case ComFieldList:
		return "COM_FIELD_LIST"
	case ComCreateDB:
		return "COM_CREATE_DB"
	case ComDropDB:
		return "COM_DROP_DB"
	case ComRefresh:
		return "COM_REFRESH"
	case ComShutdown:
		return "COM_SHUTDOWN"
	case ComStatistics:
		return "COM_STATISTICS"
	case ComProcessInfo:
		return "COM_PROCESS_INFO"
	case ComConnect:
		return "COM_CONNECT"
	case ComProcessKill:
		return "COM_PROCESS_KILL"
	case ComDebug:
		return "COM_DEBUG"
	case ComPing:
		return "COM_PING"
	case ComChangeUser:
		return "COM_CHANGE_USER"
	case ComStmtPrepare:
		return "COM_STMT_PREPARE"
	case ComStmtExecute:
		return "COM_STMT_EXECUTE"
	case ComStmtSendLongData:
		return "COM_STMT_SEND_LONG_DATA"
	case ComStmtClose:
		return "COM_STMT_CLOSE"
	case ComStmtReset:
		return "COM_STMT_RESET"
	case ComSetOption:
		return "COM_SET_OPTION"
	case ComStmtFetch:
		return "COM_STMT_FETCH"
	default:
		return "UNKNOWN"
	}
}

// Response header bytes
const (
	OKPacket    byte = 0x00
	EOFPacket   byte = 0xfe
	ERRPacket   byte = 0xff
	LocalInfile byte = 0xfb
)

// QueryCommand represents a COM_QUERY command
type QueryCommand struct {
	Query string
}

// OKResponse represents an OK_Packet
type OKResponse struct {
	AffectedRows uint64
	LastInsertID uint64
	StatusFlags  uint16
	Warnings     uint16
	Info         string
}

// ERRResponse represents an ERR_Packet
type ERRResponse struct {
	ErrorCode    uint16
	SQLState     string
	ErrorMessage string
}

// ResultSet represents a query result set
type ResultSet struct {
	ColumnCount uint64
	Columns     []ColumnDefinition
	Rows        [][]Value
}

// ColumnDefinition represents a column in a result set
type ColumnDefinition struct {
	Catalog      string
	Schema       string
	Table        string
	OrgTable     string
	Name         string
	OrgName      string
	CharacterSet uint16
	ColumnLength uint32
	ColumnType   byte
	Flags        uint16
	Decimals     byte
}

// Value represents a value in a result row
type Value struct {
	IsNull bool
	Data   []byte
}

// String returns the value as a string
func (v Value) String() string {
	if v.IsNull {
		return "NULL"
	}
	return string(v.Data)
}

// ServerHandshake represents the initial handshake from server
type ServerHandshake struct {
	ProtocolVersion byte
	ServerVersion   string
	ConnectionID    uint32
	AuthPluginData  []byte
	CapabilityFlags uint32
	CharacterSet    byte
	StatusFlags     uint16
	AuthPluginName  string
}
