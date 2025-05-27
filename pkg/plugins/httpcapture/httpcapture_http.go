package httpcapture

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/rulekitext"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"
	"github.com/qpoint-io/rulekit"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type instance struct {
	logger *zap.Logger

	ctx        context.Context
	conn       plugins.PluginContext
	macros     rulekitsvc.Macros
	eventstore eventstore.EventStore

	level  CaptureLevel
	format OutputFormat
	rules  []LogRule

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
	if i.eventstore == nil {
		i.logger.Error("eventstore is nil, cannot save HTTP transaction")
		return
	}

	// Determine the capture level based on rules
	captureLevel := i.level
	outputFormat := i.format

	// Check if any rules match and should override default capture level
	if len(i.rules) > 0 {
		// Create rule evaluation pairs from request, response headers and metadata
		reqPairs := tools.NewHeaderMap(i.reqheaders).RulePairs("request")
		resPairs := tools.NewHeaderMap(i.resheaders).RulePairs("response")
		metaPairs := tools.MetadataRulePairs(i.conn.Metadata())

		// Combine all pairs for rule evaluation
		allPairs := make(map[string]any, len(reqPairs)+len(resPairs)+len(metaPairs))

		// Add all pairs using maps.Copy
		maps.Copy(allPairs, reqPairs)
		maps.Copy(allPairs, resPairs)
		maps.Copy(allPairs, metaPairs)

		var macros map[string]rulekit.Rule
		if i.macros != nil {
			macros = i.macros.Macros()
		}

		// Evaluate rules in order
		for _, r := range i.rules {
			if r.rule == nil {
				continue
			}

			res := r.rule.Eval(&rulekit.Ctx{
				Functions: rulekitext.Functions,
				Macros:    macros,
				KV:        allPairs,
			})
			if !res.Ok() {
				// log any non-ErrMissingFields errors
				mf := &rulekit.ErrMissingFields{}
				if !errors.As(res.Error, &mf) {
					i.logger.Error("error evaluating rule",
						zap.Error(res.Error),
						zap.String("evaluated_rule", res.EvaluatedRule.String()),
					)
				}
				continue
			}
			if res.Pass() {
				captureLevel = r.Level
				// Override format if specified in the rule
				if r.Format != "" {
					outputFormat = r.Format
				}
				break
			}
		}
	}

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

	// Create the artifact
	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_HTTPTransaction,
		Data:        data,
		ContentType: contentType,
	}

	// Set metadata
	setArtifactMetadata(artifact, i.conn)

	// Save the artifact to the eventstore
	i.logger.Debug("saving HTTP transaction to eventstore",
		zap.String("level", string(captureLevel)),
		zap.String("format", string(outputFormat)),
		zap.Int("bytes", len(data)))

	i.eventstore.Save(ctx, artifact)
}

// setArtifactMetadata sets metadata from the connection context on the artifact
func setArtifactMetadata(artifact *eventstore.Artifact, ctx plugins.PluginContext) {
	if ctx == nil || artifact == nil {
		return
	}

	// Set connection ID if available
	if connID := ctx.GetMetadata("connection-id").String(); connID != "" {
		artifact.SetConnectionID(connID)
	}

	// Set endpoint ID if available
	if endpointID := ctx.GetMetadata("endpoint-id").String(); endpointID != "" {
		artifact.SetEndpointID(endpointID)
	}

	// Set request ID if available from the metadata
	if requestID := ctx.GetMetadata("qpoint-request-id").String(); requestID != "" {
		artifact.SetRequestID(requestID)
	}
}
