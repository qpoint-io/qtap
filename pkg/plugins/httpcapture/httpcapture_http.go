package httpcapture

import (
	"context"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/rulekitext"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"
	"github.com/qpoint-io/rulekit"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type instance struct {
	logger *zap.Logger

	// services
	ctx         context.Context
	conn        plugins.PluginContext
	rulekit     rulekitsvc.Service
	objectstore objectstore.ObjectStore
	level       CaptureLevel
	format      OutputFormat
	rules       []LogRule

	reqheaders plugins.Headers
	resheaders plugins.Headers
	startTime  time.Time
	endTime    time.Time
}

func (i *instance) RequestHeaders(requestHeaders plugins.Headers, endOfStream bool) plugins.HeadersStatus {
	// Set the start time when request headers are received
	i.startTime = time.Now()
	i.reqheaders = requestHeaders
	return plugins.HeadersStatusContinue
}

func (i *instance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	// For "full" capture level, we need to buffer the request body
	if !endOfStream {
		return plugins.BodyStatusStopIterationAndBuffer
	}
	return plugins.BodyStatusContinue
}

func (i *instance) ResponseHeaders(responseHeaders plugins.Headers, endOfStream bool) plugins.HeadersStatus {
	// Set response headers and end time
	i.endTime = time.Now()
	i.resheaders = responseHeaders
	return plugins.HeadersStatusContinue
}

func (i *instance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	// For "full" capture level, we need to buffer the response body
	if !endOfStream {
		return plugins.BodyStatusStopIterationAndBuffer
	}
	return plugins.BodyStatusContinue
}

func (i *instance) Destroy() {
	ctx, span := tracer.Start(i.ctx, "Destroy")
	defer func() {
		span.End()
		// end parent filterInstance span
		defer trace.SpanFromContext(i.ctx).End()
	}()

	// If eventstore is not available, log an error and return
	if i.objectstore == nil {
		i.logger.Error("objectstore is nil, cannot save HTTP transaction")
		return
	}

	// Determine the capture level based on rules
	captureLevel, outputFormat := i.evaluateRules()

	// If capture level is none, don't capture anything
	if captureLevel == CaptureLevelNone {
		i.logger.Debug("HTTP transaction capture skipped due to 'none' capture level")
		return
	}

	// Create HTTP transaction object
	transaction := NewHttpTransaction(i.conn, i.reqheaders, i.resheaders, i.startTime, i.endTime)

	// Set the appropriate content based on the capture level
	switch captureLevel {
	case CaptureLevelSummary:
		// Summary level doesn't include headers or bodies, so clear them
		transaction.Request.Headers = nil
		transaction.Response.Headers = nil
		transaction.Request.Body = nil
		transaction.Response.Body = nil

	case CaptureLevelHeaders:
		// Headers level includes headers but not bodies
		transaction.Request.Body = nil
		transaction.Response.Body = nil

	case CaptureLevelFull:
		// Full level includes everything - add the request and response bodies
		transaction.Request.Body = i.conn.GetRequestBodyBuffer().Copy()
		transaction.Response.Body = i.conn.GetResponseBodyBuffer().Copy()
	}

	// Generate the appropriate format
	var data []byte
	var contentType string

	switch outputFormat {
	case OutputFormatJSON:
		var err error
		data, err = transaction.ToJSON()
		if err != nil {
			i.logger.Error("failed to marshal HTTP transaction to JSON", zap.Error(err))
			return
		}
		contentType = "application/json"

	case OutputFormatText:
		data = []byte(transaction.ToString())
		contentType = "text/plain"

	default:
		i.logger.Error("unknown output format", zap.String("format", string(outputFormat)))
		return
	}

	meta := i.conn.Meta()
	// Create the artifact
	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_HTTPTransaction,
		Data:        data,
		ContentType: contentType,
	}
	artifact.SetRequestID(meta.RequestID())

	// Attach the summary to the artifact
	if summary := transaction.Summary(); len(summary) > 0 {
		artifact.Summary = summary
	}

	// Save the artifact to the objectstore
	i.logger.Debug("saving HTTP transaction to object store",
		zap.String("level", string(captureLevel)),
		zap.String("format", string(outputFormat)),
		zap.Int("bytes", len(data)))

	i.objectstore.Put(ctx, artifact)
}

func (i *instance) evaluateRules() (CaptureLevel, OutputFormat) {
	level, format := i.level, i.format
	if len(i.rules) == 0 {
		return level, format
	}

	// Check if any rules match and should override default capture level
	if i.rulekit == nil {
		i.logger.Warn("rulekit service is missing, cannot evaluate rules. using default capture level and format")
		return level, format
	}

	// Evaluate rules in order
	kvs := tools.ConnectionValues(i.rulekit, i.reqheaders, i.resheaders)
	for _, r := range i.rules {
		if r.rule == nil {
			continue
		}

		res := r.rule.Eval(&rulekit.Ctx{
			Functions: i.rulekit.Functions(),
			Macros:    i.rulekit.Macros(),
			KV:        kvs,
		})
		if !res.Ok() {
			if rulekitext.IsCriticalErr(res.Error) {
				i.logger.Error("error evaluating rule",
					zap.Error(res.Error),
					zap.String("evaluated_rule", res.EvaluatedRule.String()))
			}
			continue
		}
		if res.Pass() {
			level = r.Level
			// Override format if specified in the rule
			if r.Format != "" {
				format = r.Format
			}
			break
		}
	}

	return level, format
}
