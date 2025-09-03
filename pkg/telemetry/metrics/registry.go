package metrics

import (
	"net/http"
	"runtime"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// productRegistry holds user-facing business metrics
	productRegistry = prometheus.NewRegistry()

	// systemRegistry holds internal operational metrics
	systemRegistry = prometheus.NewRegistry()
)

func init() {
	systemRegistry.MustRegister(collectors.NewGoCollector())
	systemRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// ProductRegistry returns the registry for user-facing product metrics
func ProductRegistry() *prometheus.Registry {
	return productRegistry
}

// SystemRegistry returns the registry for internal system metrics
func SystemRegistry() *prometheus.Registry {
	return systemRegistry
}

// ProductHandler returns an HTTP handler for product metrics
func ProductHandler() http.Handler {
	return promhttp.HandlerFor(productRegistry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// SystemHandler returns an HTTP handler for system/operational metrics
func SystemHandler() http.Handler {
	return promhttp.HandlerFor(systemRegistry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Handler returns the default product metrics handler for backward compatibility
func Handler() http.Handler {
	return ProductHandler()
}

// getPackageName returns the package name of the caller
func getPackageName(skip int) string {
	pc, _, _, _ := runtime.Caller(1 + skip)
	funcName := runtime.FuncForPC(pc).Name()

	// Find the last slash to get past any path components
	lastSlash := max(strings.LastIndexByte(funcName, '/'), 0)

	// Extract the part after the last slash (package.function or package.(*Type).method)
	packageAndFunc := funcName[lastSlash+1:]

	// Find the first dot to separate package from function/method
	firstDot := strings.IndexByte(packageAndFunc, '.')
	if firstDot == -1 {
		// No dot found, return the whole thing
		return packageAndFunc
	}

	// Return just the package name (everything before the first dot)
	return packageAndFunc[:firstDot]
}
