package httpcapture

import (
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// grpcInstance embeds instance to reuse all HTTP plugin hook methods.
// Only Destroy() is overridden to build a GrpcTransaction instead of HttpTransaction.
type grpcInstance struct {
	instance
}

func (g *grpcInstance) Destroy() {
	ctx, span := tracer.Start(g.ctx, "Destroy")
	defer func() {
		span.End()
		// end parent instance span
		defer trace.SpanFromContext(g.ctx).End()
	}()

	if g.objectstore == nil {
		g.logger.Error("objectstore is nil, cannot save gRPC transaction")
		return
	}

	// Determine the capture level based on rules
	captureLevel, outputFormat := g.evaluateRules()

	if captureLevel == CaptureLevelNone {
		g.logger.Debug("gRPC transaction capture skipped due to 'none' capture level")
		return
	}

	// Create gRPC transaction object
	transaction := NewGrpcTransaction(g.conn, g.reqheaders, g.resheaders, g.startTime, g.endTime)

	// Set content based on capture level
	switch captureLevel {
	case CaptureLevelSummary:
		transaction.Request.Headers = nil
		transaction.Response.Headers = nil
		transaction.Request.Body = nil
		transaction.Response.Body = nil

	case CaptureLevelHeaders:
		transaction.Request.Body = nil
		transaction.Response.Body = nil

	case CaptureLevelFull:
		transaction.Request.Body = g.conn.GetRequestBodyBuffer().Copy()
		transaction.Response.Body = g.conn.GetResponseBodyBuffer().Copy()
	}

	// Generate the appropriate format
	var data []byte
	var contentType string

	switch outputFormat {
	case OutputFormatJSON:
		var err error
		data, err = transaction.ToJSON()
		if err != nil {
			g.logger.Error("failed to marshal gRPC transaction to JSON", zap.Error(err))
			return
		}
		contentType = "application/json"

	case OutputFormatText:
		data = []byte(transaction.ToString())
		contentType = "text/plain"

	default:
		g.logger.Error("unknown output format", zap.String("format", string(outputFormat)))
		return
	}

	meta := g.conn.Meta()
	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_GRPCTransaction,
		Data:        data,
		ContentType: contentType,
	}
	artifact.SetRequestID(meta.RequestID())

	if summary := transaction.Summary(); len(summary) > 0 {
		artifact.Summary = summary
	}

	g.logger.Debug("saving gRPC transaction to object store",
		zap.String("level", string(captureLevel)),
		zap.String("format", string(outputFormat)),
		zap.Int("bytes", len(data)))

	g.objectstore.Put(ctx, artifact)
}
