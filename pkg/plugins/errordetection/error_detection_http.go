package errordetection

import (
	"context"
	"encoding/json"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var tracer = telemetry.Tracer()

type filterInstance struct {
	logger *zap.Logger
	ctx    context.Context
	conn   plugins.PluginContext

	eventstore  eventstore.EventStore
	objectstore objectstore.ObjectStore
	rules       []Rule

	tags func() []string

	shouldRecordReqBody bool
	reqBody             []byte
	resBody             []byte

	startTime  time.Time
	finishTime time.Time

	reqheaders plugins.Headers
	resheaders plugins.Headers

	now func() time.Time
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.startTime = h.now()
	h.reqheaders = headers

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if !endOfStream && h.shouldRecordReqBody {
		// Wait for the end of the stream to see the full body.
		return plugins.BodyStatusStopIterationAndBuffer
	}

	// Now we can read the entire body.
	h.reqBody = h.conn.GetRequestBodyBuffer().Copy()

	// TODO: move detection logic Destroy() to here so we can clear the buffer asap if no rules are triggered

	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	// set the finish time
	h.finishTime = h.now()
	h.resheaders = headers

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if !endOfStream {
		// Wait for the end of the stream to see the full body.
		return plugins.BodyStatusStopIterationAndBuffer
	}

	// Now we can read the entire body.
	h.resBody = h.conn.GetResponseBodyBuffer().Copy()

	// TODO: move detection logic from Destroy() to here so we can clear the buffer asap if no rules are triggered

	return plugins.BodyStatusContinue
}

func (h *filterInstance) Destroy() {
	ctx, span := tracer.Start(h.ctx, "Destroy")
	defer func() {
		span.End()
		// end parent filterInstance span
		defer trace.SpanFromContext(h.ctx).End()
	}()

	reqHeaders := tools.NewHeaderMap(h.reqheaders)
	resHeaders := tools.NewHeaderMap(h.resheaders)

	meta := h.conn.Meta()
	requestID := meta.RequestID()
	logger := h.logger.With(zap.String("request_id", requestID))

	url, _ := reqHeaders.URL()
	path, _ := reqHeaders.Path()
	method, _ := reqHeaders.Method()
	status, _ := resHeaders.Status()
	direction := meta.Direction()

	triggeredArtifactPersists := RulePersists{}

	for _, rule := range h.rules {
		// if no error detected for a rule continue
		rs := rule.Matcher(logger)
		triggers, match := rs.Match(
			httpMessage{reqHeaders, h.reqBody, h.tags(), h.duration()},
			httpMessage{resHeaders, h.resBody, h.tags(), h.duration()},
		)
		if !match {
			continue
		}

		logger.Debug("rule triggered", zap.String("rule_name", rule.Name), zap.Any("rule_triggers", triggers))

		triggeredArtifactPersists.RecordReqBody = triggeredArtifactPersists.RecordReqBody || rule.RecordReqBody
		triggeredArtifactPersists.RecordReqHeaders = triggeredArtifactPersists.RecordReqHeaders || rule.RecordReqHeaders
		triggeredArtifactPersists.RecordResBody = triggeredArtifactPersists.RecordResBody || rule.RecordResBody
		triggeredArtifactPersists.RecordResHeaders = triggeredArtifactPersists.RecordResHeaders || rule.RecordResHeaders

		// log issues
		if rule.ReportAsIssue {
			issue := &eventstore.Issue{
				Timestamp:         h.now(),
				Direction:         direction,
				Error:             rule.Name,
				URL:               url,
				URLPath:           path,
				Method:            method,
				Status:            status,
				TriggerConditions: TriggerConditions(triggers).ToConditions(),
				TriggerReasons:    TriggerConditions(triggers).ToReasons(),
			}
			issue.SetRequestID(requestID)

			h.eventstore.Save(ctx, issue)
		}
	}

	if triggeredArtifactPersists.Any() {
		logger.Debug("saving artifacts for request", zap.Any("persisted_artifacts", triggeredArtifactPersists))
	} else {
		logger.Debug("no artifact storage indicated")
	}

	// Save request body if indicated
	if triggeredArtifactPersists.RecordReqBody && len(h.reqBody) > 0 {
		logger.Debug("saving request body")
		ct, ok := reqHeaders.ContentType()
		if !ok {
			ct = "text/plain"
		}

		artifact := &eventstore.Artifact{
			Type:        eventstore.ArtifactType_RequestBody,
			Data:        h.reqBody,
			ContentType: ct,
		}
		artifact.SetRequestID(requestID)

		h.objectstore.Put(ctx, artifact)
	}

	// Save request headers if inidicated
	if triggeredArtifactPersists.RecordReqHeaders {
		logger.Debug("saving request headers")
		if h.reqheaders == nil {
			logger.Debug("no request headers to save")
			return
		}
		data, err := json.Marshal(h.reqheaders.All())
		if err != nil {
			logger.Error("marshalling request headers", zap.Error(err))
			return
		}

		artifact := &eventstore.Artifact{
			Type:        eventstore.ArtifactType_RequestHeaders,
			Data:        data,
			ContentType: "application/json",
		}
		artifact.SetRequestID(requestID)

		h.objectstore.Put(ctx, artifact)
	}

	// Save response body if indicated
	if triggeredArtifactPersists.RecordResBody && len(h.resBody) > 0 {
		logger.Debug("saving response body")
		ct, ok := resHeaders.ContentType()
		if !ok {
			ct = "text/plain"
		}

		artifact := &eventstore.Artifact{
			Type:        eventstore.ArtifactType_ResponseBody,
			Data:        h.resBody,
			ContentType: ct,
		}
		artifact.SetRequestID(requestID)

		h.objectstore.Put(ctx, artifact)
	}

	// Save response headers if inidicated
	if triggeredArtifactPersists.RecordResHeaders {
		logger.Debug("saving response headers")
		data, err := json.Marshal(h.resheaders.All())
		if err != nil {
			logger.Error("marshalling response headers", zap.Error(err))
			return
		}

		artifact := &eventstore.Artifact{
			Type:        eventstore.ArtifactType_ResponseHeaders,
			Data:        data,
			ContentType: "application/json",
		}
		artifact.SetRequestID(requestID)

		h.objectstore.Put(ctx, artifact)
	}
	logger.Debug("plugin instance destroyed")
}

func (h *filterInstance) duration() time.Duration {
	var dur time.Duration
	if !h.startTime.IsZero() && !h.finishTime.IsZero() {
		dur = h.finishTime.Sub(h.startTime)
		if dur == 0 {
			dur = time.Millisecond * 1
		}
	}

	return dur
}

type httpMessage struct {
	headers  *tools.HeaderMap
	body     []byte
	tags     []string
	duration time.Duration
}

func (m httpMessage) Headers() *tools.HeaderMap {
	return m.headers
}

func (m httpMessage) Body() []byte {
	return m.body
}

func (m httpMessage) Tags() []string {
	return m.tags
}

func (m httpMessage) Duration() time.Duration {
	return m.duration
}
