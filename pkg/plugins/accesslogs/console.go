package accesslogs

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"go.uber.org/zap"
	"golang.org/x/term"
)

var (
	borderWidthCap = 50

	bodyStartToken  = "cc4f6a29-ba6d-488b-9cd4-a2656bc4d07f"
	bodyEndToken    = "1afc4ffd-340c-4d7d-8972-fde4261b4569"
	colorMuted      = lipgloss.AdaptiveColor{Light: "#b0b0b0", Dark: "#737373"}
	labelStyle      = lipgloss.NewStyle().Foreground(colorMuted)
	valueStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	subsectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
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

	summaryLine := " " + buildSummary(bin, direction, method, url, status)
	fmt.Fprintln(accessLogsWriter, summaryLine)
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
	// host, _ := reqHeaders.Authority()
	path, _ := reqHeaders.Path()
	resHeaders := tools.NewHeaderMap(c.resheaders)
	status, _ := resHeaders.Status()

	protocol := meta.Protocol()
	direction := meta.Direction()

	var bin string
	if p := meta.Process(); p != nil {
		bin = p.Exe
	}

	// Build manual border at terminal width
	termWidth := terminalWidth()
	borderWidth := termWidth - 2 // Account for left/right border chars
	if borderWidth < 0 {
		borderWidth = 0
	} else if borderWidth > borderWidthCap {
		borderWidth = borderWidthCap
	}
	statusColor := getStatusColor(status)
	borderStyle := lipgloss.NewStyle().Foreground(statusColor)
	leftBorder := borderStyle.Render("│")
	paddedLeftBorder := leftBorder + "  "

	// Build sections
	var output strings.Builder

	/*
		top border
	*/
	output.WriteString(borderStyle.Render("╭"+strings.Repeat("─", borderWidth)+"┄") + "\n")

	/*
		summary
	*/
	output.WriteString(prefixLines(buildSummary(bin, direction, method, url, status), paddedLeftBorder) + "\n")

	/*
		meta
	*/
	output.WriteString(buildSectionDivider(statusColor, "", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMetaContent(meta), paddedLeftBorder) + "\n")

	/*
		request
	*/
	output.WriteString(buildSectionDivider(statusColor, "REQUEST "+color.New(color.Reset).Sprint(protocol), borderWidth) + "\n")
	writeReqResSections(&output, leftBorder, "  ", "\n"+subsectionStyle.Render(method)+" "+path)
	writeReqResSections(&output, leftBorder, "  ", buildReqResContent(c.ctx.GetRequestBodyBuffer(), includeBody, reqHeaders))

	/*
		response
	*/
	responseTitle := "RESPONSE " + color.New(color.Reset).Sprint(fmt.Sprintf("%d %s", status, http.StatusText(status)))
	output.WriteString(buildSectionDivider(statusColor, responseTitle, borderWidth) + "\n")
	writeReqResSections(&output, leftBorder, "  ", buildReqResContent(c.ctx.GetResponseBodyBuffer(), includeBody, resHeaders))

	/*
		bottom border
	*/
	output.WriteString(borderStyle.Render("╰" + strings.Repeat("─", borderWidth) + "┄"))

	fmt.Fprintln(accessLogsWriter, output.String())
	return nil
}

func writeReqResSections(output *strings.Builder, leftBorder, padding, content string) {
	lines := strings.Split(content, "\n")
	printBorder := true
	for _, line := range lines {
		if line == bodyStartToken {
			printBorder = false
			continue
		}
		if line == bodyEndToken {
			printBorder = true
			continue
		}
		if printBorder {
			line = leftBorder + padding + line
		}
		output.WriteString(line + "\n")
	}
}

// buildSummary creates the summary line with colored status
func buildSummary(cmd, direction, method, url string, status int) string {
	fn := getColorFn(status)
	shortCmd := filepath.Base(cmd)

	arrow := "→"
	if !strings.Contains(direction, "egress") {
		arrow = "←"
	}
	arrow = fn(arrow)

	method = lipgloss.NewStyle().Bold(true).Render(method)
	summary := fmt.Sprintf("%s %s %s %s %s %s %s",
		fn("■"), shortCmd, arrow, method, url, fn(status), fn(http.StatusText(status)))

	return summary
}

// buildSectionDivider creates a horizontal divider with section title
func buildSectionDivider(color lipgloss.Color, title string, width int) string {
	titleStyle := lipgloss.NewStyle().Foreground(color).Bold(true)

	if width < 0 {
		width = 0
	}

	if title == "" {
		return titleStyle.Render("├" + strings.Repeat("─", width) + "┄")
	}

	titleLeft := "├───╼ " + title
	titleWidth := lipgloss.Width(titleLeft)

	remainingWidth := max(width-titleWidth, 0)
	var titleRight string
	switch {
	case remainingWidth < 2:
	case remainingWidth == 2:
		titleRight = " ╾"
	case remainingWidth > 2:
		titleRight = " ╾" + strings.Repeat("─", remainingWidth-1) + "┄"
	}

	return titleStyle.Render(titleLeft) + titleStyle.Render(titleRight)
}

// buildMetaContent creates the META section content
func buildMetaContent(meta plugins.Meta) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Process") + "\n")
	if p := meta.Process(); p != nil {
		sb.WriteString(bulletPoint(
			"",
			labelStyle.Render("PID ")+valueStyle.Render(strconv.Itoa(p.Pid))+
				labelStyle.Render(" @ ")+valueStyle.Render(p.Exe),
		))

		if c, _ := p.Container(); c != nil {
			sb.WriteString(bulletPoint("Container", c.Name))
			sb.WriteString(bulletPoint("Image", c.Image))
		}
		if p, _ := p.Pod(); p != nil {
			sb.WriteString(bulletPoint("Pod", p.Name))
			sb.WriteString(bulletPoint("Namespace", p.Namespace))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Network") + "\n")
	var (
		arrow     string
		direction string
	)
	switch meta.Direction() {
	case "ingress":
		direction = "Ingress"
		arrow = "←"
	case "egress":
		direction = "Egress"
		arrow = "→"
	case "egress-internal":
		direction = "Egress (internal)"
		arrow = "→"
	case "egress-external":
		direction = "Egress (external)"
		arrow = "→"
	}
	sb.WriteString("  " + labelStyle.Render(arrow) + " " + valueStyle.Render(direction) + "\n")
	wrBytes := humanize.Bytes(uint64(meta.WriteBytes()))
	rdBytes := humanize.Bytes(uint64(meta.ReadBytes()))
	sb.WriteString(
		labelStyle.Render("  ↑ ") + valueStyle.Render(wrBytes) + labelStyle.Render(" sent") +
			labelStyle.Render("   ↓ ") + valueStyle.Render(rdBytes) + labelStyle.Render(" received") +
			"\n",
	)

	return sb.String()
}

// buildResponseContent creates the REQUEST or RESPONSE section contents
func buildReqResContent(body plugins.BodyBuffer, includeBody bool, headers *tools.HeaderMap) string {
	var sb strings.Builder
	sb.WriteString("\n")

	printHeaders(&sb, filterHeaders(headers.All()))

	// Body
	if includeBody {
		sb.WriteString("\n")
		sb.WriteString(subsectionStyle.Render("Body") + "\n")
		contentType, _ := headers.ContentType()
		if headers.BinaryContentType() {
			body := "  Body is in binary format"
			if contentType != "" {
				body += " (" + contentType + ")"
			}
			sb.WriteString(labelStyle.Render(body) + "\n")
		} else {
			if body.Length() == 0 {
				sb.WriteString(labelStyle.Render("  (empty)") + "\n")
			} else {
				printBody(&sb, contentType, body.Copy())
			}
		}
	}

	return sb.String()
}

func printHeaders(sb *strings.Builder, headers map[string]string) {
	if len(headers) == 0 {
		return
	}

	sb.WriteString(subsectionStyle.Render("Headers") + "\n")
	for key, value := range headers {
		sb.WriteString(labelStyle.Render("  • "+key+": ") + valueStyle.Render(value) + "\n")
	}
}

func printBody(sb *strings.Builder, contentType string, body []byte) {
	sb.WriteString(bodyStartToken + "\n")

	for prefix, language := range map[string]string{
		"application/json":       "json",
		"text/json":              "json",
		"text/yaml":              "yaml",
		"application/yaml":       "yaml",
		"text/javascript":        "javascript",
		"application/javascript": "javascript",
		"text/html":              "html",
		"text/css":               "css",
		"application/css":        "css",
	} {
		if strings.HasPrefix(contentType, prefix) {
			if res, err := highlightCode(language, string(body)); err == nil {
				body = []byte(res)
			}
			break
		}
	}

	sb.Write(body)
	if !strings.HasSuffix(string(body), "\n") {
		sb.WriteString("\n")
	}

	sb.WriteString(bodyEndToken)
}

func filterHeaders(headers map[string]string) map[string]string {
	clean := make(map[string]string)
	for k, v := range headers {
		if strings.HasPrefix(k, ":") {
			continue
		}
		clean[k] = v
	}
	return clean
}

// getStatusColor returns lipgloss color based on HTTP status
func getStatusColor(status int) lipgloss.Color {
	switch {
	case status >= 200 && status < 300:
		return lipgloss.Color("2") // Green
	case status >= 300 && status < 400:
		return lipgloss.Color("4") // Blue
	case status >= 400 && status < 500:
		return lipgloss.Color("3") // Yellow
	case status >= 500:
		return lipgloss.Color("1") // Red
	default:
		return lipgloss.Color("7") // White
	}
}

func getColorFn(status int) func(a ...any) string {
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

func terminalWidth() int {
	termWidth := 80 // Default width
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		termWidth = width
	}
	return termWidth
}

func prefixLines(text string, prefix string) string {
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}

func bulletPoint(key, value string) string {
	if key != "" {
		key += ": "
	}
	return labelStyle.Render("  • "+key) + valueStyle.Render(value) + "\n"
}

func highlightCode(language, code string) (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0),
	)
	if err != nil {
		return "", fmt.Errorf("unable to create renderer: %w", err)
	}
	output, err := renderer.Render(fmt.Sprintf("```%s\n%s\n```", language, strings.TrimSpace(code)))
	if err != nil {
		return "", fmt.Errorf("unable to render: %w", err)
	}
	output = strings.TrimSpace(output)
	return output, nil
}
