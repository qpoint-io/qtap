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

const maxKafkaSummaryBytes = 64 * 1024 // 64KB

// kafkaFilterInstance handles Kafka traffic for reporting
type kafkaFilterInstance struct {
	logger     *zap.Logger
	ctx        plugins.PluginContext
	eventstore eventstore.EventStore

	command *plugins.KafkaCommand
	result  *plugins.KafkaResult
}

func (k *kafkaFilterInstance) OnKafkaCommand(cmd *plugins.KafkaCommand) plugins.KafkaStatus {
	k.command = cmd
	return plugins.KafkaStatusContinue
}

func (k *kafkaFilterInstance) OnKafkaResult(res *plugins.KafkaResult) plugins.KafkaStatus {
	k.result = res
	return plugins.KafkaStatusContinue
}

func (k *kafkaFilterInstance) Destroy() {
	if k.command == nil {
		return
	}

	ctx := context.TODO()
	meta := k.ctx.Meta()

	req := k.buildDatabaseRequest(k.command, k.result, meta)
	k.eventstore.Save(ctx, req)

	k.logger.Debug("kafka plugin instance destroyed")
}

func (k *kafkaFilterInstance) buildDatabaseRequest(cmd *plugins.KafkaCommand, res *plugins.KafkaResult, meta plugins.Meta) *eventstore.DatabaseRequest {
	req := &eventstore.DatabaseRequest{
		Timestamp:    time.Now().UTC(),
		Direction:    meta.Direction(),
		DatabaseType: "kafka",
		Statement:    k.buildStatement(cmd),
		WrBytes:      meta.WriteBytes(),
		RdBytes:      meta.ReadBytes(),
	}

	// Use latency from result (request-parse to response-parse round-trip).
	// Fall back to wall-clock from command timestamp only when no result is available.
	if res != nil && res.Latency > 0 {
		duration := res.Latency.Milliseconds()
		if duration == 0 {
			duration = 1
		}
		req.Duration = duration
	} else if !cmd.Timestamp.IsZero() {
		duration := time.Since(cmd.Timestamp).Milliseconds()
		if duration == 0 {
			duration = 1
		}
		req.Duration = duration
	}

	// Build response summary from command and result messages
	req.ResponseSummary = k.buildResponseSummary(cmd, res)

	// Add result details if available
	if res != nil {
		req.IsError = res.IsError
		if res.IsError {
			req.ErrorMsg = res.ErrorMessage
			req.ResultType = "Error"
		} else {
			req.ResultType = "OK"
		}
	}

	req.SetRequestID(meta.RequestID())

	return req
}

// buildStatement creates a human-readable statement from the Kafka command
func (k *kafkaFilterInstance) buildStatement(cmd *plugins.KafkaCommand) string {
	var parts []string
	parts = append(parts, cmd.Operation)
	if len(cmd.Topics) > 0 {
		parts = append(parts, strings.Join(cmd.Topics, ","))
	}
	if cmd.GroupID != "" {
		parts = append(parts, "group="+cmd.GroupID)
	}
	return strings.Join(parts, " ")
}

// buildResponseSummary creates a summary of topics and message samples
func (k *kafkaFilterInstance) buildResponseSummary(cmd *plugins.KafkaCommand, res *plugins.KafkaResult) string {
	var sb strings.Builder

	// Collect all messages (from command for Produce, from result for Fetch)
	var messages []plugins.KafkaMessage
	if len(cmd.Messages) > 0 {
		messages = cmd.Messages
	}
	if res != nil && len(res.Messages) > 0 {
		messages = append(messages, res.Messages...)
	}

	if len(messages) == 0 {
		if len(cmd.Topics) > 0 {
			sb.WriteString("topics: " + strings.Join(cmd.Topics, ", "))
		}
		return truncateKafkaSummary(sb.String(), maxKafkaSummaryBytes)
	}

	sb.WriteString(fmt.Sprintf("%d messages", len(messages)))
	for i, msg := range messages {
		if i >= 10 { // Cap at 10 message samples
			sb.WriteString(fmt.Sprintf("\n... and %d more", len(messages)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("\n  [%s/%d]", msg.Topic, msg.Partition))
		if msg.Key != "" {
			sb.WriteString(" key=" + msg.Key)
		}
		if msg.Value != "" {
			val := msg.Value
			if len(val) > 256 {
				val = val[:256] + "..."
			}
			sb.WriteString(" " + val)
		}
	}

	return truncateKafkaSummary(sb.String(), maxKafkaSummaryBytes)
}

func truncateKafkaSummary(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
