package report

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

// redisFilterInstance handles Redis traffic for reporting
type redisFilterInstance struct {
	logger     *zap.Logger
	ctx        plugins.PluginContext
	eventstore eventstore.EventStore

	command *plugins.RedisCommand
	result  *plugins.RedisResult
}

func (r *redisFilterInstance) OnRedisCommand(cmd *plugins.RedisCommand) plugins.RedisStatus {
	r.command = cmd
	return plugins.RedisStatusContinue
}

func (r *redisFilterInstance) OnRedisResult(res *plugins.RedisResult) plugins.RedisStatus {
	r.result = res
	return plugins.RedisStatusContinue
}

func (r *redisFilterInstance) Destroy() {
	if r.command == nil {
		return
	}

	ctx := context.TODO()
	meta := r.ctx.Meta()

	req := r.buildDatabaseRequest(r.command, r.result, meta)
	r.eventstore.Save(ctx, req)

	r.logger.Debug("redis plugin instance destroyed")
}

func (r *redisFilterInstance) buildDatabaseRequest(cmd *plugins.RedisCommand, res *plugins.RedisResult, meta plugins.Meta) *eventstore.DatabaseRequest {
	req := &eventstore.DatabaseRequest{
		Timestamp:    time.Now().UTC(),
		Direction:    meta.Direction(),
		DatabaseType: "redis",
		Statement:    r.buildStatement(cmd),
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
		req.IsError = res.IsError
		if res.IsError {
			if errMsg, ok := res.Value.(string); ok {
				req.ErrorMsg = errMsg
			} else {
				req.ErrorMsg = fmt.Sprintf("%v", res.Value)
			}
		}
	}

	req.SetRequestID(meta.RequestID())

	return req
}

// buildStatement creates a human-readable statement from the command and args
func (r *redisFilterInstance) buildStatement(cmd *plugins.RedisCommand) string {
	if len(cmd.Args) == 0 {
		return cmd.Name
	}
	return fmt.Sprintf("%s %s", cmd.Name, strings.Join(r.redactArgs(cmd.Name, cmd.Args), " "))
}

// redactArgs redacts sensitive arguments for security-sensitive commands
func (r *redisFilterInstance) redactArgs(command string, args []string) []string {
	upperCmd := strings.ToUpper(command)

	// Commands with sensitive data that should be redacted
	sensitiveCommands := map[string]bool{
		"AUTH":   true,
		"CONFIG": true,
	}

	if sensitiveCommands[upperCmd] {
		redacted := make([]string, len(args))
		for i := range args {
			redacted[i] = "[REDACTED]"
		}
		return redacted
	}

	return args
}
