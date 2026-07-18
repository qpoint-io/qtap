package qscan

import (
	"encoding/json"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

type kafkaFilterInstance struct {
	logger      *zap.Logger
	ctx         plugins.PluginContext
	objectstore objectstore.ObjectStore
	config      *QscanConfig
	factory     *Factory
	sample      bool

	requestID string
	statement string
	command   *plugins.KafkaCommand
	result    *plugins.KafkaResult
}

func (h *kafkaFilterInstance) OnKafkaCommand(cmd *plugins.KafkaCommand) plugins.KafkaStatus {
	scansAttemptedTotal.Inc()

	h.statement = plugins.BuildKafkaStatement(cmd)

	var sampleReason string
	h.sample, sampleReason = h.factory.shouldSampleDB(h.statement)

	if h.sample {
		scansSampledTotal.WithLabelValues(sampleReason).Inc()
	} else {
		scansNotSampledTotal.Inc()
	}

	h.command = cmd
	h.requestID = h.ctx.Meta().RequestID()

	return plugins.KafkaStatusContinue
}

func (h *kafkaFilterInstance) OnKafkaResult(res *plugins.KafkaResult) plugins.KafkaStatus {
	h.result = res
	return plugins.KafkaStatusContinue
}

func (h *kafkaFilterInstance) Destroy() {
	if !h.sample {
		return
	}

	if h.command == nil {
		return
	}

	txn := &DBTransactionRequest{
		TransactionType: "db",
		DBType:          "kafka",
		Query:           h.statement,
		RequestID:       h.requestID,
	}

	// Collect messages from both command and result
	var messages []plugins.KafkaMessage
	if len(h.command.Messages) > 0 {
		messages = append(messages, h.command.Messages...)
	}
	if h.result != nil {
		if len(h.result.Messages) > 0 {
			messages = append(messages, h.result.Messages...)
		}
		if h.result.IsError {
			txn.Error = &DBError{
				Code:    int(h.result.ErrorCode),
				Message: h.result.ErrorMessage,
			}
		}
	}

	if len(messages) > 0 {
		txn.ResultSet = make([]map[string]any, 0, len(messages))
		for _, msg := range messages {
			txn.ResultSet = append(txn.ResultSet, map[string]any{
				"topic":     msg.Topic,
				"partition": msg.Partition,
				"key":       msg.Key,
				"value":     msg.Value,
			})
		}
	}

	jsonData, err := json.Marshal(txn)
	if err != nil {
		h.logger.Error("failed to marshal qscan db document", zap.Error(err))
		scansStoredTotal.WithLabelValues("error").Inc()
		return
	}

	if len(jsonData) > defaultCaptureByteLimit {
		h.logger.Warn("qscan db request is too large, skipping", zap.Int("size", len(jsonData)))
		return
	}

	scansStoredSize.Observe(float64(len(jsonData)))

	a := eventstore.Artifact{
		Type:        eventstore.ArtifactType_QscanRequest,
		Data:        jsonData,
		ContentType: "application/json",
		Summary:     h.config.Summary(),
	}
	a.SetEndpointID(h.ctx.Meta().Endpoint())
	a.SetRequestID(h.requestID)

	if h.objectstore != nil {
		h.objectstore.Put(h.ctx.Context(), &a)
		scansStoredTotal.WithLabelValues("success").Inc()
	} else {
		scansStoredTotal.WithLabelValues("error").Inc()
	}
}
