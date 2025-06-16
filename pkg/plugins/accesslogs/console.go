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
	meta := c.ctx.Meta()
	reqHeaders := tools.NewHeaderMap(c.reqheaders)
	url, _ := reqHeaders.URL()
	method, _ := reqHeaders.Method()

	var bin string
	if p := meta.Process(); p != nil {
		bin = p.Exe
	}

	direction := meta.Direction()
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
	meta := c.ctx.Meta()
	reqHeaders := tools.NewHeaderMap(c.reqheaders)
	url, _ := reqHeaders.URL()
	method, _ := reqHeaders.Method()
	host, _ := reqHeaders.Authority()
	protocol := meta.Protocol()
	direction := meta.Direction()
	var bin string

	mp := map[string]string{
		"direction": direction,
		"wr_bytes":  strconv.FormatInt(meta.WriteBytes(), 10),
		"rd_bytes":  strconv.FormatInt(meta.ReadBytes(), 10),
	}

	if p := meta.Process(); p != nil {
		bin = p.Exe
		mp["pid"] = strconv.Itoa(p.Pid)
		mp["exe"] = p.Exe
		if c, _ := p.Container(); c != nil {
			mp["container_name"] = c.Name
			mp["container_image"] = c.Image
		}
		if p, _ := p.Pod(); p != nil {
			mp["pod_name"] = p.Name
			mp["pod_namespace"] = p.Namespace
		}
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
