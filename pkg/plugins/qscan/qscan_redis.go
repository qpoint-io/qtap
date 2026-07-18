package qscan

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

type redisFilterInstance struct {
	logger      *zap.Logger
	ctx         plugins.PluginContext
	objectstore objectstore.ObjectStore
	config      *QscanConfig
	factory     *Factory
	sample      bool

	requestID string
	statement string
	command   *plugins.RedisCommand
	result    *plugins.RedisResult
}

func (h *redisFilterInstance) OnRedisCommand(cmd *plugins.RedisCommand) plugins.RedisStatus {
	scansAttemptedTotal.Inc()

	h.statement = buildRedisStatement(cmd)

	var sampleReason string
	h.sample, sampleReason = h.factory.shouldSampleDB(h.statement)

	if h.sample {
		scansSampledTotal.WithLabelValues(sampleReason).Inc()
	} else {
		scansNotSampledTotal.Inc()
	}

	h.command = cmd
	h.requestID = h.ctx.Meta().RequestID()

	return plugins.RedisStatusContinue
}

func (h *redisFilterInstance) OnRedisResult(res *plugins.RedisResult) plugins.RedisStatus {
	h.result = res
	return plugins.RedisStatusContinue
}

func (h *redisFilterInstance) Destroy() {
	if !h.sample {
		return
	}

	if h.command == nil {
		return
	}

	txn := &DBTransactionRequest{
		TransactionType: "db",
		DBType:          "redis",
		Query:           h.statement,
		RequestID:       h.requestID,
	}

	if h.result != nil {
		if h.result.IsError {
			txn.Error = &DBError{
				Message: fmt.Sprintf("%v", h.result.Value),
			}
		} else {
			txn.ResultSet = []map[string]any{
				{
					"type":  h.result.Type,
					"value": fmt.Sprintf("%v", h.result.Value),
				},
			}
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

func buildRedisStatement(cmd *plugins.RedisCommand) string {
	if len(cmd.Args) == 0 {
		return cmd.Name
	}
	return cmd.Name + " " + strings.Join(cmd.Args, " ")
}
