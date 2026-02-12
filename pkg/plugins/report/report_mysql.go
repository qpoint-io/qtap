package report

import (
	"context"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

// mysqlFilterInstance handles MySQL traffic for reporting
type mysqlFilterInstance struct {
	logger     *zap.Logger
	ctx        plugins.PluginContext
	eventstore eventstore.EventStore

	command *plugins.MySQLCommand
	result  *plugins.MySQLResult
}

func (m *mysqlFilterInstance) OnMySQLCommand(cmd *plugins.MySQLCommand) plugins.MySQLStatus {
	m.command = cmd
	return plugins.MySQLStatusContinue
}

func (m *mysqlFilterInstance) OnMySQLResult(res *plugins.MySQLResult) plugins.MySQLStatus {
	m.result = res
	return plugins.MySQLStatusContinue
}

func (m *mysqlFilterInstance) Destroy() {
	if m.command == nil {
		return
	}

	ctx := context.TODO()
	meta := m.ctx.Meta()

	req := m.buildDatabaseRequest(m.command, m.result, meta)
	m.eventstore.Save(ctx, req)

	m.logger.Debug("mysql plugin instance destroyed")
}

func (m *mysqlFilterInstance) buildDatabaseRequest(cmd *plugins.MySQLCommand, res *plugins.MySQLResult, meta plugins.Meta) *eventstore.DatabaseRequest {
	req := &eventstore.DatabaseRequest{
		Timestamp:    time.Now().UTC(),
		Direction:    meta.Direction(),
		DatabaseType: "mysql",
		Statement:    cmd.Query,
		WrBytes:      meta.WriteBytes(),
		RdBytes:      meta.ReadBytes(),
	}

	// Calculate duration if we have timestamp info
	if !cmd.Timestamp.IsZero() {
		duration := time.Since(cmd.Timestamp).Milliseconds()
		if duration == 0 {
			duration = 1
		}
		req.Duration = duration
	}

	// Add result details if available
	if res != nil {
		req.ResultType = res.Type
		req.IsError = res.Type == "Error"
		if req.IsError {
			req.ErrorMsg = res.ErrorMessage
		}
		req.AffectedCount = int64(res.AffectedRows)
		req.ResultCount = int64(res.RowCount)
	}

	req.SetRequestID(meta.RequestID())

	return req
}
