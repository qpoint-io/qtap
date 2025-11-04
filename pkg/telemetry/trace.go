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

func Tracer() trace.Tracer {
	pkg, _ := callerInfo(1)
	return otel.Tracer(pkg)
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
