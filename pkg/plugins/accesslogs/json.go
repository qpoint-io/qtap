package accesslogs

import (
	"strconv"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"go.uber.org/zap"
)

// JSONPrinter implements the 	 interface for JSON format
type JSONPrinter struct {
	ctx        plugins.PluginContext
	reqheaders plugins.Headers
	resheaders plugins.Headers
	logger     *zap.Logger
	writer     *zap.Logger
}

// NewJSONPrinter creates a new JSONPrinter instance
func NewJSONPrinter(
	ctx plugins.PluginContext,
	reqheaders plugins.Headers,
	resheaders plugins.Headers,
	logger *zap.Logger,
	writer *zap.Logger,
) *JSONPrinter {
	return &JSONPrinter{
		ctx:        ctx,
		reqheaders: reqheaders,
		resheaders: resheaders,
		logger:     logger,
		writer:     writer,
	}
}

// PrintSummary implements Printer.PrintSummary
func (j *JSONPrinter) PrintSummary() {
	meta := j.ctx.Meta()
	reqHeaders := tools.NewHeaderMap(j.reqheaders)
	url, _ := reqHeaders.URL()
	method, _ := reqHeaders.Method()

	requestID := meta.RequestID()
	var bin string
	if p := meta.Process(); p != nil {
		bin = p.Exe
	}

	direction := meta.Direction()

	resHeaders := tools.NewHeaderMap(j.resheaders)
	status, _ := resHeaders.Status()

	j.writer.Info("HTTP transaction",
		zap.String("bin", bin),
		zap.String("direction", direction),
		zap.String("method", method),
		zap.String("url", url),
		zap.Int("status", status),
		zap.String("request_id", requestID))
}

// PrintDetails implements Printer.PrintDetails
func (j *JSONPrinter) PrintDetails() error {
	return j.printJSONDetails(false)
}

// PrintFull implements Printer.PrintFull
func (j *JSONPrinter) PrintFull() error {
	return j.printJSONDetails(true)
}

func (j *JSONPrinter) printJSONDetails(includeBody bool) error {
	meta := j.ctx.Meta()
	reqHeaders := tools.NewHeaderMap(j.reqheaders)
	requestID := meta.RequestID()
	url, _ := reqHeaders.URL()
	method, _ := reqHeaders.Method()
	protocol := meta.Protocol()

	direction := meta.Direction()

	resHeaders := tools.NewHeaderMap(j.resheaders)
	status, _ := resHeaders.Status()

	req := map[string]any{
		"url":        url,
		"method":     method,
		"proto":      protocol,
		"request_id": requestID,
		"headers":    j.reqheaders.All(),
	}
	if includeBody {
		if reqHeaders.BinaryContentType() {
			req["body"] = "<binary>"
		} else {
			req["body"] = string(j.ctx.GetRequestBodyBuffer().Copy())
		}
	}

	resp := map[string]any{
		"status":  status,
		"headers": j.resheaders.All(),
	}
	if includeBody {
		if resHeaders.BinaryContentType() {
			resp["body"] = "<binary>"
		} else {
			resp["body"] = string(j.ctx.GetResponseBodyBuffer().Copy())
		}
	}

	values := map[string]any{
		"bytes_sent":     strconv.FormatInt(meta.WriteBytes(), 10),
		"bytes_received": strconv.FormatInt(meta.ReadBytes(), 10),
	}

	if p := meta.Process(); p != nil {
		values["pid"] = p.Pid
		values["exe"] = p.Exe

		if c, _ := p.Container(); c != nil {
			values["container_name"] = c.Name
			values["container_image"] = c.Image
		}

		if p, _ := p.Pod(); p != nil {
			values["pod_name"] = p.Name
			values["pod_namespace"] = p.Namespace
		}
	}

	j.writer.Info("HTTP transaction",
		zap.Any("meta", values),
		zap.Any("direction", direction),
		zap.Any("request", req),
		zap.Any("response", resp),
	)

	return nil
}
