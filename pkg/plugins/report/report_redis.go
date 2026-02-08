package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const maxResponseSummaryBytes = 64 * 1024 // 64KB

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
		if !res.IsError && res.Value != nil {
			req.ResponseSummary = formatRedisValue(res.Value)
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

// formatRedisValue converts a Redis response value to a human-readable summary, bounded to 64KB.
func formatRedisValue(v any) string {
	var s string
	switch val := v.(type) {
	case string:
		s = val
	case []any:
		b, err := json.Marshal(val)
		if err != nil {
			s = fmt.Sprintf("%v", val)
		} else {
			s = string(b)
		}
	case int64:
		s = strconv.FormatInt(val, 10)
	default:
		s = fmt.Sprintf("%v", val)
	}
	if len(s) > maxResponseSummaryBytes {
		s = s[:maxResponseSummaryBytes]
	}
	return s
}
