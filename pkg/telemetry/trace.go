package telemetry

import (
	"context"
	"net/http"
	"runtime"
	"strings"

	"github.com/qpoint-io/qtap/pkg/buildinfo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

type TraceProvider struct {
	tracer trace.Tracer
}

func Tracer() *TraceProvider {
	pkg, _ := callerInfo(1)
	return &TraceProvider{
		tracer: otel.Tracer(pkg),
	}
}

func (tp *TraceProvider) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tp.tracer.Start(ctx, name, opts...)
}

// WithoutCancel starts a new span as a child of the parent context while detaching the cancel function from the parent context.
func (tp *TraceProvider) WithoutCancel(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := tp.tracer.Start(ctx, name, opts...)
	ctx = context.WithoutCancel(ctx)
	return ctx, span
}

// WithRemoteCancel starts a new span as a child of the parent context with the cancel function from a different context.
func (tp *TraceProvider) WithRemoteCancel(ctx context.Context, cancelCtx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := tp.tracer.Start(ctx, name, opts...)
	ctx = remoteCancelCtx{parentCtx: ctx, cancelCtx: cancelCtx}
	return ctx, span
}

func (tp *TraceProvider) StartFn(
	ctx context.Context,
	name string,
	opts []trace.SpanStartOption,
	fn func(ctx context.Context, span trace.Span) error,
) error {
	ctx, span := tp.Start(ctx, name, opts...)
	defer span.End()
	err := fn(ctx, span)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func callerInfo(skip int) (pkg, fn string) {
	pc, _, _, _ := runtime.Caller(1 + skip)
	funcName := runtime.FuncForPC(pc).Name()
	lastSlash := strings.LastIndexByte(funcName, '/')
	if lastSlash < 0 {
		lastSlash = 0
	}
	lastDot := strings.LastIndexByte(funcName[lastSlash:], '.') + lastSlash

	pkg = funcName[:lastDot]
	fn = funcName[lastDot+1:]

	return
}

func OtelResource(ctx context.Context, name string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNamespaceKey.String("qpoint")),
		resource.WithAttributes(semconv.ServiceNameKey.String(name)),
		resource.WithAttributes(semconv.ServiceVersionKey.String(buildinfo.Version())),
		resource.WithAttributes(semconv.ServiceInstanceIDKey.String(Hostname())),
	)
}

func InstrumentHTTPClient(client *http.Client) {
	client.Transport = otelhttp.NewTransport(client.Transport)
}

func WithBaggage(ctx context.Context, values map[string]string) context.Context {
	members := make([]baggage.Member, 0, len(values))
	for k, v := range values {
		m, _ := baggage.NewMember(k, v)
		members = append(members, m)
	}
	bag, _ := baggage.New(members...)
	return baggage.ContextWithBaggage(ctx, bag)
}
