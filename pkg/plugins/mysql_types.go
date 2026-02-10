package plugins

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
)

// MySQLStatus indicates how plugin iteration should proceed
type MySQLStatus int

const (
	MySQLStatusContinue      MySQLStatus = 0
	MySQLStatusStopIteration MySQLStatus = 1
)

// MySQLCommand represents a MySQL command received from the client
type MySQLCommand struct {
	Type      byte      // Command type (e.g., 0x03 for COM_QUERY)
	Query     string    // SQL query text (for COM_QUERY, COM_STMT_PREPARE)
	StmtID    uint32    // Statement ID (for COM_STMT_EXECUTE)
	Params    []any     // Bound parameters (for prepared statements)
	Timestamp time.Time
}

// MySQLResult represents a MySQL result received from the server
type MySQLResult struct {
	Type         string // "OK", "Error", "ResultSet", "EOF"
	AffectedRows uint64
	LastInsertID uint64
	ErrorCode    uint16
	ErrorMessage string
	Columns      []string   // Column names
	Rows         [][]string // Row values (string representation)
	RowCount     int        // Total rows (may exceed len(Rows) if truncated)
	Truncated    bool       // True if rows were capped at MaxResultSetRows
}

// MySQLPluginInstance handles MySQL traffic for a single connection
type MySQLPluginInstance interface {
	PluginInstance
	OnMySQLCommand(cmd *MySQLCommand) MySQLStatus
	OnMySQLResult(res *MySQLResult) MySQLStatus
}

// MySQLPlugin is the capability interface for plugins that handle MySQL traffic
type MySQLPlugin interface {
	Plugin // Embeds base
	NewMySQLInstance(PluginContext, *services.ServiceRegistry) MySQLPluginInstance
}
