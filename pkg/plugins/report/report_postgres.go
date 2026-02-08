package report

import (
	"context"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

// postgresFilterInstance handles PostgreSQL traffic for reporting
type postgresFilterInstance struct {
	logger     *zap.Logger
	ctx        plugins.PluginContext
	eventstore eventstore.EventStore

	command *plugins.PostgresCommand
	result  *plugins.PostgresResult
}

func (p *postgresFilterInstance) OnPostgresCommand(cmd *plugins.PostgresCommand) plugins.PostgresStatus {
	p.command = cmd
	return plugins.PostgresStatusContinue
}

func (p *postgresFilterInstance) OnPostgresResult(res *plugins.PostgresResult) plugins.PostgresStatus {
	p.result = res
	return plugins.PostgresStatusContinue
}

func (p *postgresFilterInstance) Destroy() {
	if p.command == nil {
		return
	}

	ctx := context.TODO()
	meta := p.ctx.Meta()

	req := p.buildDatabaseRequest(p.command, p.result, meta)
	p.eventstore.Save(ctx, req)

	p.logger.Debug("postgres plugin instance destroyed")
}

func (p *postgresFilterInstance) buildDatabaseRequest(cmd *plugins.PostgresCommand, res *plugins.PostgresResult, meta plugins.Meta) *eventstore.DatabaseRequest {
	req := &eventstore.DatabaseRequest{
		Timestamp:    time.Now().UTC(),
		Direction:    meta.Direction(),
		DatabaseType: "postgresql",
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
		req.ResultCount = res.RowCount
		req.Columns = res.Columns
		req.Rows = res.Rows
		req.Truncated = res.Truncated
	}

	req.SetRequestID(meta.RequestID())

	return req
}
