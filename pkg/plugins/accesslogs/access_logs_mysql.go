package accesslogs

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"go.uber.org/zap"
)

// mysqlFilterInstance handles MySQL traffic for access logging
type mysqlFilterInstance struct {
	logger *zap.Logger
	writer *zap.Logger

	ctx    plugins.PluginContext
	mode   displayMode
	format outputFormat

	commands []*plugins.MySQLCommand
	results  []*plugins.MySQLResult
}

func (f *mysqlFilterInstance) OnMySQLCommand(cmd *plugins.MySQLCommand) plugins.MySQLStatus {
	f.commands = append(f.commands, cmd)
	return plugins.MySQLStatusContinue
}

func (f *mysqlFilterInstance) OnMySQLResult(res *plugins.MySQLResult) plugins.MySQLStatus {
	f.results = append(f.results, res)
	return plugins.MySQLStatusContinue
}

func (f *mysqlFilterInstance) Destroy() {
	if f.mode == displayModeNone {
		return
	}

	// Print each command/result pair
	for i, cmd := range f.commands {
		var res *plugins.MySQLResult
		if i < len(f.results) {
			res = f.results[i]
		}

		printer := NewMySQLPrinter(f.format, f.ctx, cmd, res, f.logger, f.writer)
		switch f.mode {
		case displayModeSummary:
			printer.PrintSummary()
		case displayModeDetails, displayModeFull:
			printer.PrintDetails()
		default:
			printer.PrintSummary()
		}
	}
}

// MySQLPrinter defines the interface for MySQL access log formatting
type MySQLPrinter interface {
	PrintSummary()
	PrintDetails()
}

// NewMySQLPrinter creates a new MySQL printer based on the specified format
func NewMySQLPrinter(
	format outputFormat,
	ctx plugins.PluginContext,
	cmd *plugins.MySQLCommand,
	res *plugins.MySQLResult,
	logger *zap.Logger,
	writer *zap.Logger,
) MySQLPrinter {
	switch format {
	case outputFormatJSON:
		return &mysqlJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	case outputFormatConsole:
		return &mysqlConsolePrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	default:
		return &mysqlJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	}
}

// mysqlJSONPrinter implements MySQLPrinter for JSON format
type mysqlJSONPrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.MySQLCommand
	res    *plugins.MySQLResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *mysqlJSONPrinter) PrintSummary() {
	meta := p.ctx.Meta()
	requestID := meta.RequestID()
	direction := meta.Direction()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	fields := []zap.Field{
		zap.String("bin", bin),
		zap.String("direction", direction),
		zap.String("query", truncateQuery(p.cmd.Query, 100)),
		zap.String("request_id", requestID),
	}

	if p.res != nil {
		fields = append(fields,
			zap.String("result_type", p.res.Type),
			zap.Bool("is_error", p.res.Type == "Error"),
		)
		if p.res.AffectedRows > 0 {
			fields = append(fields, zap.Uint64("affected_rows", p.res.AffectedRows))
		}
		if p.res.ErrorCode > 0 {
			fields = append(fields, zap.Uint16("error_code", p.res.ErrorCode))
		}
	}

	p.writer.Info("MySQL transaction", fields...)
}

func (p *mysqlJSONPrinter) PrintDetails() {
	meta := p.ctx.Meta()
	requestID := meta.RequestID()
	direction := meta.Direction()

	command := map[string]any{
		"query":     p.cmd.Query,
		"timestamp": p.cmd.Timestamp,
	}

	var result map[string]any
	if p.res != nil {
		result = map[string]any{
			"type":     p.res.Type,
			"is_error": p.res.Type == "Error",
		}
		if p.res.AffectedRows > 0 {
			result["affected_rows"] = p.res.AffectedRows
		}
		if p.res.LastInsertID > 0 {
			result["last_insert_id"] = p.res.LastInsertID
		}
		if p.res.ErrorCode > 0 {
			result["error_code"] = p.res.ErrorCode
			result["error_message"] = p.res.ErrorMessage
		}
	}

	values := map[string]any{
		"request_id": requestID,
	}

	if proc := meta.Process(); proc != nil {
		values["pid"] = proc.Pid
		values["exe"] = proc.Exe

		if c, _ := proc.Container(); c != nil {
			values["container_name"] = c.Name
			values["container_image"] = c.Image
		}

		if pod, _ := proc.Pod(); pod != nil {
			values["pod_name"] = pod.Name
			values["pod_namespace"] = pod.Namespace
		}
	}

	fields := []zap.Field{
		zap.Any("meta", values),
		zap.String("direction", direction),
		zap.Any("command", command),
	}
	if result != nil {
		fields = append(fields, zap.Any("result", result))
	}

	p.writer.Info("MySQL transaction", fields...)
}

// mysqlConsolePrinter implements MySQLPrinter for console format
type mysqlConsolePrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.MySQLCommand
	res    *plugins.MySQLResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *mysqlConsolePrinter) PrintSummary() {
	meta := p.ctx.Meta()
	direction := meta.Direction()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	summaryLine := " " + buildMySQLSummary(bin, direction, p.cmd, p.res)
	fmt.Fprintln(accessLogsWriter, summaryLine)
}

func (p *mysqlConsolePrinter) PrintDetails() {
	meta := p.ctx.Meta()
	direction := meta.Direction()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	// Build manual border at terminal width
	termWidth := terminalWidth()
	borderWidth := termWidth - 2
	if borderWidth < 0 {
		borderWidth = 0
	} else if borderWidth > borderWidthCap {
		borderWidth = borderWidthCap
	}

	borderColor := getMySQLStatusColor(p.res)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	leftBorder := borderStyle.Render("│")
	paddedLeftBorder := leftBorder + "  "

	var output strings.Builder

	// Top border
	output.WriteString(borderStyle.Render("╭"+strings.Repeat("─", borderWidth)+"┄") + "\n")

	// Summary
	output.WriteString(prefixLines(buildMySQLSummary(bin, direction, p.cmd, p.res), paddedLeftBorder) + "\n")

	// Meta section
	output.WriteString(buildSectionDivider(borderColor, "", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMetaContent(meta), paddedLeftBorder) + "\n")

	// Query section
	output.WriteString(buildSectionDivider(borderColor, "QUERY", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMySQLQueryContent(p.cmd), paddedLeftBorder) + "\n")

	// Result section
	if p.res != nil {
		resultTitle := "RESULT"
		if p.res.Type == "Error" {
			resultTitle = "RESULT (ERROR)"
		}
		output.WriteString(buildSectionDivider(borderColor, resultTitle, borderWidth) + "\n")
		output.WriteString(prefixLines(buildMySQLResultContent(p.res), paddedLeftBorder) + "\n")
	}

	// Bottom border
	output.WriteString(borderStyle.Render("╰" + strings.Repeat("─", borderWidth) + "┄"))

	fmt.Fprintln(accessLogsWriter, output.String())
}

// buildMySQLSummary creates the summary line for MySQL commands
func buildMySQLSummary(cmd, direction string, mysqlCmd *plugins.MySQLCommand, res *plugins.MySQLResult) string {
	fn := getMySQLColorFn(res)
	shortCmd := filepath.Base(cmd)

	arrow := "→"
	if !strings.Contains(direction, "egress") {
		arrow = "←"
	}
	arrow = fn(arrow)

	// Extract query type (SELECT, INSERT, etc.)
	queryType := extractQueryType(mysqlCmd.Query)
	queryType = lipgloss.NewStyle().Bold(true).Render(queryType)

	// Truncate query for summary
	queryPreview := truncateQuery(mysqlCmd.Query, 50)

	var status string
	if res != nil {
		if res.Type == "Error" {
			status = fn("ERR")
		} else {
			status = fn("OK")
		}
	}

	summary := fmt.Sprintf("%s %s %s %s %s %s",
		fn("■"), shortCmd, arrow, queryType, queryPreview, status)

	return summary
}

func buildMySQLQueryContent(cmd *plugins.MySQLCommand) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Query") + "\n")

	// Format query nicely
	query := cmd.Query
	if len(query) > 500 {
		query = query[:497] + "..."
	}
	sb.WriteString(labelStyle.Render("  • SQL: ") + valueStyle.Render(query) + "\n")
	sb.WriteString(labelStyle.Render("  • Time: ") + valueStyle.Render(cmd.Timestamp.Format("15:04:05.000")) + "\n")

	return sb.String()
}

func buildMySQLResultContent(res *plugins.MySQLResult) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Result") + "\n")
	sb.WriteString(labelStyle.Render("  • Type: ") + valueStyle.Render(res.Type) + "\n")

	if res.AffectedRows > 0 {
		sb.WriteString(labelStyle.Render("  • Affected Rows: ") + valueStyle.Render(strconv.FormatUint(res.AffectedRows, 10)) + "\n")
	}
	if res.LastInsertID > 0 {
		sb.WriteString(labelStyle.Render("  • Last Insert ID: ") + valueStyle.Render(strconv.FormatUint(res.LastInsertID, 10)) + "\n")
	}
	if res.Type == "Error" {
		sb.WriteString(labelStyle.Render("  • Error Code: ") + valueStyle.Render(strconv.FormatUint(uint64(res.ErrorCode), 10)) + "\n")
		sb.WriteString(labelStyle.Render("  • Error: ") + valueStyle.Render(res.ErrorMessage) + "\n")
	}

	return sb.String()
}

// getMySQLStatusColor returns lipgloss color based on MySQL result
func getMySQLStatusColor(res *plugins.MySQLResult) lipgloss.Color {
	if res == nil {
		return lipgloss.Color("7") // White - no result yet
	}
	if res.Type == "Error" {
		return lipgloss.Color("1") // Red
	}
	return lipgloss.Color("2") // Green
}

func getMySQLColorFn(res *plugins.MySQLResult) func(a ...interface{}) string {
	if res == nil {
		return color.New(color.FgWhite).SprintFunc()
	}
	if res.Type == "Error" {
		return color.New(color.FgRed).SprintFunc()
	}
	return color.New(color.FgGreen).SprintFunc()
}

// truncateQuery truncates a query to maxLen characters
func truncateQuery(query string, maxLen int) string {
	query = strings.TrimSpace(query)
	query = strings.ReplaceAll(query, "\n", " ")
	query = strings.ReplaceAll(query, "\t", " ")
	if len(query) > maxLen {
		return query[:maxLen-3] + "..."
	}
	return query
}

// extractQueryType extracts the SQL command type from a query
func extractQueryType(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "UNKNOWN"
	}

	// Find first word
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return "UNKNOWN"
	}

	return strings.ToUpper(parts[0])
}
