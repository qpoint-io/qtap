package connection

import (
	"context"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func (c *Connection) logConnectionReport() {
	span := trace.SpanFromContext(c.ctx)
	var errorMsgs []string
	var logFn func(msg string, fields ...zap.Field) = c.logger.Debug

	fields := []zap.Field{
		zap.Any("conn_key", c.Key()),
		zap.String("destinationProtocol", c.OpenEvent.SocketType.String()),
		zap.Dict("report", c.report.reportFields()...),
	}

	// add handler type
	fields = append(fields, zap.String("handler", c.HandlerType.String()))
	span.SetAttributes(attribute.String("connection.handler", c.HandlerType.String()))

	// add strategy
	if proc := c.Process(); proc != nil {
		span.SetAttributes(attribute.String("connection.strategy", proc.Strategy.String()))
		fields = append(fields, zap.String("strategy", proc.Strategy.String()))
	}

	if len(errorMsgs) > 0 {
		fields = append(fields, zap.Strings("errors", errorMsgs))
	}

	// send it
	logFn("connection report", fields...)
}

type report struct {
	ctx                    context.Context
	openTime               time.Time
	closeTime              time.Time
	dataEventCount         uint64
	gotOrigDestEvent       bool
	gotTLSClientHelloEvent bool
	gotProtocolEvent       bool
	gotHandlerTypeEvent    bool
}

// reportEvent is called when the event is first received
func (r *report) reportEvent(event any) {
	span := trace.SpanFromContext(r.ctx)
	eventName := strings.TrimPrefix(reflect.TypeOf(event).String(), "connection.")
	var eventAttrs []attribute.KeyValue

	switch v := event.(type) {
	case OpenEvent:
		r.openTime = time.Now()
	case CloseEvent:
		r.closeTime = time.Now()
	case ProtocolEvent:
		eventAttrs = append(eventAttrs, attribute.String("protocol", v.Protocol.String()))
		r.gotProtocolEvent = true
	case DataEvent:
		eventAttrs = append(eventAttrs, attribute.Int("data_event.size", v.Size))
		r.dataEventCount++
	case TLSClientHelloEvent:
		r.gotTLSClientHelloEvent = true
	case OriginalDestinationEvent:
		r.gotOrigDestEvent = true
	case HandlerTypeEvent:
		r.gotHandlerTypeEvent = true
	}

	span.AddEvent(eventName, trace.WithAttributes(eventAttrs...))
}

func (r *report) reportFields() []zap.Field {
	return []zap.Field{
		zap.Duration("duration", r.closeTime.Sub(r.openTime)),
		zap.Bool("gotTLSClientHelloEvent", r.gotTLSClientHelloEvent),
		zap.Bool("gotProtocolEvent", r.gotProtocolEvent),
		zap.Bool("gotHandlerTypeEvent", r.gotHandlerTypeEvent),
		zap.Uint64("dataEventCount", r.dataEventCount),
	}
}
