package report

import (
	"context"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

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
		Statement:    plugins.BuildKafkaStatement(cmd),
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

	req.ResponseSummary = plugins.BuildKafkaResponseSummary(cmd, res)

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
