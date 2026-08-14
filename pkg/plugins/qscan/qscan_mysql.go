package qscan

import (
	"encoding/json"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

const (
	comQuery       byte = 0x03
	comStmtPrepare byte = 0x16
)

type mysqlFilterInstance struct {
	logger      *zap.Logger
	ctx         plugins.PluginContext
	objectstore objectstore.ObjectStore
	config      *QscanConfig
	factory     *Factory
	sample      bool

	requestID string
	command   *plugins.MySQLCommand
	result    *plugins.MySQLResult
}

func (h *mysqlFilterInstance) OnMySQLCommand(cmd *plugins.MySQLCommand) plugins.MySQLStatus {
	scansAttemptedTotal.Inc()

	if cmd.Type != comQuery && cmd.Type != comStmtPrepare {
		return plugins.MySQLStatusContinue
	}

	if cmd.Query == "" {
		return plugins.MySQLStatusContinue
	}

	var sampleReason string
	h.sample, sampleReason = h.factory.shouldSampleDB(cmd.Query)

	if h.sample {
		scansSampledTotal.WithLabelValues(sampleReason).Inc()
	} else {
		scansNotSampledTotal.Inc()
	}

	h.command = cmd
	h.requestID = h.ctx.Meta().RequestID()

	return plugins.MySQLStatusContinue
}

func (h *mysqlFilterInstance) OnMySQLResult(res *plugins.MySQLResult) plugins.MySQLStatus {
	h.result = res
	return plugins.MySQLStatusContinue
}

func (h *mysqlFilterInstance) Destroy() {
	if !h.sample {
		return
	}

	if h.command == nil {
		return
	}

	txn := &DBTransactionRequest{
		TransactionType: "db",
		DBType:          "mysql",
		Query:           h.command.Query,
		RequestID:       h.requestID,
	}

	if h.result != nil {
		switch h.result.Type {
		case "ResultSet":
			if len(h.result.Columns) > 0 {
				txn.ResultSet = zipMySQLRows(h.result.Columns, h.result.Rows)
			}
			if h.result.Truncated {
				txn.Truncated = true
			}
		case "OK":
			txn.ResultSet = []map[string]any{
				{
					"affected_rows":  h.result.AffectedRows,
					"last_insert_id": h.result.LastInsertID,
				},
			}
		case "Error":
			txn.Error = &DBError{
				Code:    int(h.result.ErrorCode),
				Message: h.result.ErrorMessage,
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

func zipMySQLRows(columns []string, rows [][]string) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m := make(map[string]any, len(columns))
		for i, col := range columns {
			if i < len(row) {
				m[col] = row[i]
			}
		}
		result = append(result, m)
	}
	return result
}
