package postgres

// PostgreSQL Wire Protocol Types
// Reference: https://www.postgresql.org/docs/current/protocol-message-formats.html

// Frontend (client → server) message types
const (
	// Typed messages (have a type byte prefix)
	MsgQuery        byte = 'Q' // Simple query
	MsgParse        byte = 'P' // Extended query: prepare statement
	MsgBind         byte = 'B' // Extended query: bind parameters
	MsgExecute      byte = 'E' // Extended query: execute portal
	MsgDescribe     byte = 'D' // Describe prepared statement or portal
	MsgClose        byte = 'C' // Close prepared statement or portal
	MsgSync         byte = 'S' // Synchronization point
	MsgFlush        byte = 'H' // Force backend to flush output
	MsgTerminate    byte = 'X' // Close connection
	MsgPassword     byte = 'p' // Password/SASL response
	MsgFunctionCall byte = 'F' // Direct function invocation (legacy)
	MsgCopyData     byte = 'd' // COPY data
	MsgCopyDone     byte = 'c' // COPY complete
	MsgCopyFail     byte = 'f' // COPY failed
)

// Backend (server → client) message types
const (
	MsgAuthentication           byte = 'R' // Authentication request/ok
	MsgParameterStatus          byte = 'S' // Runtime parameter
	MsgBackendKeyData           byte = 'K' // Process ID + secret key
	MsgReadyForQuery            byte = 'Z' // Ready for new query
	MsgRowDescription           byte = 'T' // Column metadata
	MsgDataRow                  byte = 'D' // One row of result data
	MsgCommandComplete          byte = 'C' // SQL command completed
	MsgErrorResponse            byte = 'E' // Error
	MsgNoticeResponse           byte = 'N' // Warning/notice
	MsgEmptyQueryResponse       byte = 'I' // Response to empty query
	MsgParseComplete            byte = '1' // Extended: parse succeeded
	MsgBindComplete             byte = '2' // Extended: bind succeeded
	MsgCloseComplete            byte = '3' // Extended: close succeeded
	MsgParameterDescription     byte = 't' // Extended: parameter types
	MsgNoData                   byte = 'n' // Extended: statement returns no data
	MsgPortalSuspended          byte = 's' // Execute hit row limit
	MsgNotificationResponse     byte = 'A' // LISTEN/NOTIFY notification
	MsgFunctionCallResponse     byte = 'V' // Legacy function call result
	MsgNegotiateProtocolVersion byte = 'v' // Version negotiation
	MsgCopyInResponse           byte = 'G' // Ready to receive COPY data
	MsgCopyOutResponse          byte = 'H' // Sending COPY data
	MsgCopyBothResponse         byte = 'W' // Bidirectional COPY (replication)
)

// Special message codes (no type byte)
const (
	SSLRequestCode    uint32 = 80877103 // 1234 << 16 | 5679
	CancelRequestCode uint32 = 80877102 // 1234 << 16 | 5678
	GSSENCRequestCode uint32 = 80877104 // 1234 << 16 | 5680
)

// Protocol versions
const (
	ProtocolVersion30 uint32 = 196608 // 3.0: 3 << 16 | 0
	ProtocolVersion32 uint32 = 196610 // 3.2: 3 << 16 | 2
)

// Authentication subtypes (all have type byte 'R')
const (
	AuthOk                uint32 = 0
	AuthKerberosV5        uint32 = 2
	AuthCleartextPassword uint32 = 3
	AuthMD5Password       uint32 = 5
	AuthGSS               uint32 = 7
	AuthGSSContinue       uint32 = 8
	AuthSSPI              uint32 = 9
	AuthSASL              uint32 = 10
	AuthSASLContinue      uint32 = 11
	AuthSASLFinal         uint32 = 12
)

// ReadyForQuery transaction status
const (
	TxStatusIdle   byte = 'I' // Not in transaction
	TxStatusInTx   byte = 'T' // In transaction block
	TxStatusFailed byte = 'E' // In failed transaction block
)

// Message represents a parsed PostgreSQL protocol message
type Message struct {
	Type    byte   // 0 for untyped messages (startup, SSL, cancel)
	Length  uint32 // Length including itself (from wire format)
	Payload []byte // Raw payload bytes (after the length field)
}
