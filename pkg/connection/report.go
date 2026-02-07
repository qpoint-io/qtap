package connection

import (
	"context"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type report struct {
	ctx                    context.Context
	openTime               time.Time
	closeTime              time.Time
	dataEventCount         uint64
	gotOrigDestEvent       bool
	gotTLSClientHelloEvent bool
	gotTLSServerHelloEvent bool
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
	case TLSServerHelloEvent:
		r.gotTLSServerHelloEvent = true
	case OriginalDestinationEvent:
		r.gotOrigDestEvent = true
	case HandlerTypeEvent:
		r.gotHandlerTypeEvent = true
	}

	span.AddEvent(eventName, trace.WithAttributes(eventAttrs...))
}

func (r *report) DataEventCount() uint64 {
	return r.dataEventCount
}
