package plugins

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
)

// PostgresStatus indicates how plugin iteration should proceed
type PostgresStatus int

const (
	PostgresStatusContinue      PostgresStatus = 0
	PostgresStatusStopIteration PostgresStatus = 1
)

// PostgresCommand represents a PostgreSQL command received from the client
type PostgresCommand struct {
	Query     string    // SQL query text (from Query or Parse message)
	Timestamp time.Time
}

// PostgresResult represents a PostgreSQL result received from the server
type PostgresResult struct {
	Type         string // "CommandComplete", "Error", "EmptyQuery"
	CommandTag   string // e.g., "SELECT 5", "INSERT 0 1"
	ErrorCode    string // SQLSTATE code (5 chars)
	ErrorMessage string
	RowCount     int64  // parsed from command tag
}

// PostgresPluginInstance handles PostgreSQL traffic for a single connection
type PostgresPluginInstance interface {
	PluginInstance
	OnPostgresCommand(cmd *PostgresCommand) PostgresStatus
	OnPostgresResult(res *PostgresResult) PostgresStatus
}

// PostgresPlugin is the capability interface for plugins that handle PostgreSQL traffic
type PostgresPlugin interface {
	Plugin // Embeds base
	NewPostgresInstance(PluginContext, *services.ServiceRegistry) PostgresPluginInstance
}
