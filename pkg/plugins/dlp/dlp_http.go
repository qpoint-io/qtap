package dlp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

type filterInstance struct {
	logger *zap.Logger
	ctx    plugins.PluginContext

	eventstore  eventstore.EventStore
	objectstore objectstore.ObjectStore

	config *DLPConfig
	engine *DLPEngine
	rules  []Rule

	reqheaders plugins.Headers
	resheaders plugins.Headers
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.reqheaders = headers

	// NOTE: We are ignore the response headers for now.
	h.engine.ProcessHeaders(headers)

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if len(h.config.Rules) == 0 {
		return plugins.BodyStatusContinue
	}

	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.resheaders = headers

	// NOTE: We are ignore the response headers for now.
	h.engine.ProcessHeaders(headers)

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	return plugins.BodyStatusContinue
}

func (h *filterInstance) Destroy() {
	ctx := context.TODO()

	reqHeaders := tools.NewHeaderMap(h.reqheaders)

	meta := h.ctx.Meta()
	requestID := meta.RequestID()
	url, _ := reqHeaders.URL()
	path, _ := reqHeaders.Path()
	method, _ := reqHeaders.Method()
	status, _ := reqHeaders.Status()
	direction := meta.Direction()

	if counts := h.engine.DetectionCounts; len(counts) > 0 {
		for label, count := range counts {
			rule := h.engine.GetRule(label)

			if rule == nil {
				continue
			}

			// log issues
			issue := &eventstore.Issue{
				Timestamp: time.Now(),
				Direction: direction,
				Error:     fmt.Sprintf("DLP: %s Found: %d", label, count),
				URL:       url,
				URLPath:   path,
				Method:    method,
				Status:    status,
			}

			issue.SetRequestID(requestID)
			h.eventstore.Save(ctx, issue)
		}
	}

	if records := h.engine.MatchDetails; len(records) > 0 {
		jsonb, err := json.Marshal(records)
		if err != nil {
			h.logger.Error("failed to marshal match detail records", zap.Error(err))
			return
		}

		h.objectstore.Put(ctx, &eventstore.Artifact{
			Type:        eventstore.ArtifactType_DLPMatches,
			Data:        jsonb,
			ContentType: "application/json",
		})
	}
}
