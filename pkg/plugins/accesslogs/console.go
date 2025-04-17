package accesslogs

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"go.uber.org/zap"
)

// ConsolePrinter implements the Printer interface for console output
type ConsolePrinter struct {
	ctx        plugins.PluginContext
	reqheaders plugins.Headers
	resheaders plugins.Headers
	logger     *zap.Logger
	writer     *zap.Logger
}

// NewConsolePrinter creates a new ConsolePrinter instance
func NewConsolePrinter(
	ctx plugins.PluginContext,
	reqheaders plugins.Headers,
	resheaders plugins.Headers,
	logger *zap.Logger,
	writer *zap.Logger,
) *ConsolePrinter {
	return &ConsolePrinter{
		ctx:        ctx,
		reqheaders: reqheaders,
		resheaders: resheaders,
		logger:     logger,
		writer:     writer,
	}
}

// PrintSummary implements Printer.PrintSummary
func (c *ConsolePrinter) PrintSummary() {
	reqHeaders := tools.NewHeaderMap(c.reqheaders)
	url, _ := reqHeaders.URL()
	method, _ := reqHeaders.Method()
	bin := c.ctx.GetMetadata("process-bin").String()

	direction := "egress-external"
	if d := c.ctx.GetMetadata("direction"); d.OK() {
		direction = d.String()
	}

	resHeaders := tools.NewHeaderMap(c.resheaders)
	status, _ := resHeaders.Status()

	fmt.Fprintln(accessLogsWriter, summary(bin, direction, method, url, status))
}

// PrintDetails implements Printer.PrintDetails
func (c *ConsolePrinter) PrintDetails() error {
	return c.printConsoleDetails(false)
}

// PrintFull implements Printer.PrintFull
func (c *ConsolePrinter) PrintFull() error {
	return c.printConsoleDetails(true)
}

func (c *ConsolePrinter) printConsoleDetails(includeBody bool) error {
	reqHeaders := tools.NewHeaderMap(c.reqheaders)
	url, _ := reqHeaders.URL()
	method, _ := reqHeaders.Method()
	host, _ := reqHeaders.Authority()
	protocol := c.ctx.GetMetadata("protocol").String()
	bin := c.ctx.GetMetadata("process-bin").String()

	direction := "egress-external"
	if d := c.ctx.GetMetadata("direction"); d.OK() {
		direction = d.String()
	}

	wrBytes := c.ctx.GetMetadata("wr_bytes").Int64()
	rdBytes := c.ctx.GetMetadata("rd_bytes").Int64()

	mp := map[string]string{
		"direction": direction,
		"pid":       c.ctx.GetMetadata("process-pid").String(),
		"exe":       c.ctx.GetMetadata("process-exe").String(),
		// "container_id": c.ctx.GetMetadata("process-container_id").String(),
		// "pod_id":       c.ctx.GetMetadata("process-pod_id").String(),
		"container_name":  c.ctx.GetMetadata("process-container_name").String(),
		"container_image": c.ctx.GetMetadata("process-container_image").String(),
		"pod_name":        c.ctx.GetMetadata("process-pod_name").String(),
		"pod_namespace":   c.ctx.GetMetadata("process-pod_namespace").String(),
		"wr_bytes":        strconv.FormatInt(wrBytes, 10),
		"rd_bytes":        strconv.FormatInt(rdBytes, 10),
	}

	resHeaders := tools.NewHeaderMap(c.resheaders)
	status, _ := resHeaders.Status()

	var sb strings.Builder

	sb.WriteString(header(summary(bin, direction, method, url, status)))
	sb.WriteString(printMeta(mp))
	sb.WriteString(printRequest(method, host, protocol, c.reqheaders.All()))
	if includeBody {
		sb.WriteString("\n------------------ REQUEST BODY ------------------\n")
		if reqHeaders.BinaryContentType() {
			sb.WriteString("Body is in binary format\n")
		} else {
			if c.ctx.GetRequestBodyBuffer().Length() > 0 {
				sb.Write(c.ctx.GetRequestBodyBuffer().Copy())
			} else {
				sb.WriteString("(empty)\n")
			}
		}
	}
	var respHeaders map[string]string
	if c.resheaders != nil {
		respHeaders = c.resheaders.All()
	}
	sb.WriteString(printResponse(status, respHeaders))
	if includeBody {
		sb.WriteString("\n------------------ RESPONSE BODY ------------------\n")
		if resHeaders.BinaryContentType() {
			sb.WriteString("Body is in binary format\n")
		} else {
			if c.ctx.GetResponseBodyBuffer().Length() > 0 {
				sb.Write(c.ctx.GetResponseBodyBuffer().Copy())
			} else {
				sb.WriteString("(empty)\n")
			}
		}
	}

	fmt.Fprintln(accessLogsWriter, sb.String())

	return nil
}

func summary(cmd, direction, method, url string, status int) string {
	fn := getColorFn(status)
	return fmt.Sprintf("%s %s %s %s %s %s %s", fn("■"), cmd, arrow(direction), method, url, fn(status), fn(http.StatusText(status)))
}

func header(summary string) string {
	// generate a band
	band := strings.Repeat("=", len(summary))

	// put them together
	return fmt.Sprintf("\n%s\n%s\n%s\n", band, summary, band)
}

func arrow(direction string) string {
	if strings.Contains(direction, "egress") {
		return "→"
	}

	return "←"
}

func printMeta(meta map[string]string) string {
	var sb strings.Builder

	sb.WriteString("\n------------------ META ------------------\n")
	fmt.Fprintf(&sb, "PID: %s\n", meta["pid"])
	fmt.Fprintf(&sb, "Exe: %s\n", meta["exe"])
	// if cid := meta["container_id"]; cid != "root" {
	// 	fmt.Fprintf(&sb, "Container ID: %s\n", cid)
	// }
	// if pid := meta["pod_id"]; pid != "" {
	// 	fmt.Fprintf(&sb, "POD ID: %s\n", pid)
	// }
	if cname := meta["container_name"]; cname != "" && cname != "<nil>" {
		fmt.Fprintf(&sb, "Container Name: %s\n", cname)
	}
	if cimage := meta["container_image"]; cimage != "" && cimage != "<nil>" {
		fmt.Fprintf(&sb, "Container Image: %s\n", cimage)
	}
	if pname := meta["pod_name"]; pname != "" && pname != "<nil>" {
		fmt.Fprintf(&sb, "Pod Name: %s\n", pname)
	}
	if pnamespace := meta["pod_namespace"]; pnamespace != "" && pnamespace != "<nil>" {
		fmt.Fprintf(&sb, "Pod Namespace: %s\n", pnamespace)
	}

	fmt.Fprintf(&sb, "Direction: %s\n", meta["direction"])
	fmt.Fprintf(&sb, "Bytes Sent: %s\n", meta["wr_bytes"])
	fmt.Fprintf(&sb, "Bytes Received: %s\n", meta["rd_bytes"])

	return sb.String()
}

func printRequest(method, url, proto string, headers map[string]string) string {
	var sb strings.Builder

	sb.WriteString("\n------------------ REQUEST ------------------\n")
	fmt.Fprintf(&sb, "%s %s %s\n", method, url, proto)
	// print headers
	if headers != nil {
		for key, value := range headers {
			sb.WriteString(key)
			sb.WriteString(": ")
			sb.WriteString(value)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("(empty)\n")
	}

	return sb.String()
}

func printResponse(status int, headers map[string]string) string {
	var sb strings.Builder

	sb.WriteString("\n------------------ RESPONSE ------------------\n")
	fmt.Fprintf(&sb, "%d %s\n", status, http.StatusText(status))
	// print headers
	if headers != nil {
		for key, value := range headers {
			sb.WriteString(key)
			sb.WriteString(": ")
			sb.WriteString(value)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("(empty)\n")
	}

	return sb.String()
}

func getColorFn(status int) func(a ...interface{}) string {
	switch {
	case status >= 200 && status < 300:
		return color.New(color.FgGreen).SprintFunc()
	case status >= 300 && status < 400:
		return color.New(color.FgBlue).SprintFunc()
	case status >= 400 && status < 500:
		return color.New(color.FgYellow).SprintFunc()
	case status >= 500:
		return color.New(color.FgRed).SprintFunc()
	default:
		return color.New(color.FgWhite).SprintFunc()
	}
}
