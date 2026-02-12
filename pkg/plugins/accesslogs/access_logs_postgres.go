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

// postgresFilterInstance handles PostgreSQL traffic for access logging
type postgresFilterInstance struct {
	logger *zap.Logger
	writer *zap.Logger

	ctx    plugins.PluginContext
	mode   displayMode
	format outputFormat

	command *plugins.PostgresCommand
	result  *plugins.PostgresResult
}

func (f *postgresFilterInstance) OnPostgresCommand(cmd *plugins.PostgresCommand) plugins.PostgresStatus {
	f.command = cmd
	return plugins.PostgresStatusContinue
}

func (f *postgresFilterInstance) OnPostgresResult(res *plugins.PostgresResult) plugins.PostgresStatus {
	f.result = res
	return plugins.PostgresStatusContinue
}

func (f *postgresFilterInstance) Destroy() {
	if f.mode == displayModeNone {
		return
	}

	if f.command == nil {
		return
	}

	printer := NewPostgresPrinter(f.format, f.ctx, f.command, f.result, f.logger, f.writer)
	switch f.mode {
	case displayModeSummary:
		printer.PrintSummary()
	case displayModeDetails, displayModeFull:
		printer.PrintDetails()
	default:
		printer.PrintSummary()
	}
}

// PostgresPrinter defines the interface for PostgreSQL access log formatting
type PostgresPrinter interface {
	PrintSummary()
	PrintDetails()
}

// NewPostgresPrinter creates a new PostgreSQL printer based on the specified format
func NewPostgresPrinter(
	format outputFormat,
	ctx plugins.PluginContext,
	cmd *plugins.PostgresCommand,
	res *plugins.PostgresResult,
	logger *zap.Logger,
	writer *zap.Logger,
) PostgresPrinter {
	switch format {
	case outputFormatJSON:
		return &postgresJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	case outputFormatConsole:
		return &postgresConsolePrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	default:
		return &postgresJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	}
}

// postgresJSONPrinter implements PostgresPrinter for JSON format
type postgresJSONPrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.PostgresCommand
	res    *plugins.PostgresResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *postgresJSONPrinter) PrintSummary() {
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
		if p.res.RowCount > 0 {
			fields = append(fields, zap.Int64("row_count", p.res.RowCount))
		}
		if p.res.ErrorCode != "" {
			fields = append(fields, zap.String("error_code", p.res.ErrorCode))
		}
	}

	p.writer.Info("PostgreSQL transaction", fields...)
}

func (p *postgresJSONPrinter) PrintDetails() {
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
		switch p.res.Type {
		case "Error":
			result["error_code"] = p.res.ErrorCode
			result["error_message"] = p.res.ErrorMessage
		case "CommandComplete":
			result["command_tag"] = p.res.CommandTag
			result["row_count"] = p.res.RowCount
			if len(p.res.Columns) > 0 {
				result["columns"] = p.res.Columns
			}
			if len(p.res.Rows) > 0 {
				result["rows"] = p.res.Rows
			}
			result["truncated"] = p.res.Truncated
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

	p.writer.Info("PostgreSQL transaction", fields...)
}

// postgresConsolePrinter implements PostgresPrinter for console format
type postgresConsolePrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.PostgresCommand
	res    *plugins.PostgresResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *postgresConsolePrinter) PrintSummary() {
	meta := p.ctx.Meta()
	direction := meta.Direction()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	summaryLine := " " + buildPostgresSummary(bin, direction, p.cmd, p.res)
	fmt.Fprintln(accessLogsWriter, summaryLine)
}

func (p *postgresConsolePrinter) PrintDetails() {
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

	borderColor := getPostgresStatusColor(p.res)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	leftBorder := borderStyle.Render("│")
	paddedLeftBorder := leftBorder + "  "

	var output strings.Builder

	// Top border
	output.WriteString(borderStyle.Render("╭"+strings.Repeat("─", borderWidth)+"┄") + "\n")

	// Summary
	output.WriteString(prefixLines(buildPostgresSummary(bin, direction, p.cmd, p.res), paddedLeftBorder) + "\n")

	// Meta section
	output.WriteString(buildSectionDivider(borderColor, "", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMetaContent(meta), paddedLeftBorder) + "\n")

	// Query section
	output.WriteString(buildSectionDivider(borderColor, "QUERY", borderWidth) + "\n")
	output.WriteString(prefixLines(buildPostgresQueryContent(p.cmd), paddedLeftBorder) + "\n")

	// Result section
	if p.res != nil {
		resultTitle := "RESULT"
		if p.res.Type == "Error" {
			resultTitle = "RESULT (ERROR)"
		}
		output.WriteString(buildSectionDivider(borderColor, resultTitle, borderWidth) + "\n")
		output.WriteString(prefixLines(buildPostgresResultContent(p.res), paddedLeftBorder) + "\n")
	}

	// Bottom border
	output.WriteString(borderStyle.Render("╰" + strings.Repeat("─", borderWidth) + "┄"))

	fmt.Fprintln(accessLogsWriter, output.String())
}

// buildPostgresSummary creates the summary line for PostgreSQL commands
func buildPostgresSummary(exe, direction string, pgCmd *plugins.PostgresCommand, res *plugins.PostgresResult) string {
	fn := getPostgresColorFn(res)
	shortCmd := filepath.Base(exe)

	arrow := "→"
	if !strings.Contains(direction, "egress") {
		arrow = "←"
	}
	arrow = fn(arrow)

	// Extract query type (SELECT, INSERT, etc.)
	queryType := extractQueryType(pgCmd.Query)
	queryType = lipgloss.NewStyle().Bold(true).Render(queryType)

	// Truncate query for summary
	queryPreview := truncateQuery(pgCmd.Query, 50)

	var status string
	if res != nil {
		switch res.Type {
		case "Error":
			status = fn("ERR")
		case "CommandComplete":
			status = fn("OK")
			if res.RowCount > 0 {
				status += " " + valueStyle.Render(strconv.FormatInt(res.RowCount, 10)+" rows")
			}
		default:
			status = fn("OK")
		}
	}

	summary := fmt.Sprintf("%s %s %s %s %s %s",
		fn("■"), shortCmd, arrow, queryType, queryPreview, status)

	return summary
}

func buildPostgresQueryContent(cmd *plugins.PostgresCommand) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Query") + "\n")

	query := cmd.Query
	if len(query) > 500 {
		query = query[:497] + "..."
	}
	sb.WriteString(labelStyle.Render("  • SQL: ") + valueStyle.Render(query) + "\n")
	sb.WriteString(labelStyle.Render("  • Time: ") + valueStyle.Render(cmd.Timestamp.Format("15:04:05.000")) + "\n")

	return sb.String()
}

func buildPostgresResultContent(res *plugins.PostgresResult) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Result") + "\n")
	sb.WriteString(labelStyle.Render("  • Type: ") + valueStyle.Render(res.Type) + "\n")

	switch res.Type {
	case "CommandComplete":
		sb.WriteString(labelStyle.Render("  • Command Tag: ") + valueStyle.Render(res.CommandTag) + "\n")
		if res.RowCount > 0 {
			sb.WriteString(labelStyle.Render("  • Row Count: ") + valueStyle.Render(strconv.FormatInt(res.RowCount, 10)) + "\n")
		}
		if res.Truncated {
			sb.WriteString(labelStyle.Render("  • (showing first ") + valueStyle.Render(strconv.Itoa(len(res.Rows))) + labelStyle.Render(" rows)") + "\n")
		}
		if len(res.Columns) > 0 && len(res.Rows) > 0 {
			sb.WriteString("\n")
			sb.WriteString(formatPostgresResultTable(res.Columns, res.Rows, res.NullFlags))
		}
	case "Error":
		sb.WriteString(labelStyle.Render("  • Error Code: ") + valueStyle.Render(res.ErrorCode) + "\n")
		sb.WriteString(labelStyle.Render("  • Error: ") + valueStyle.Render(res.ErrorMessage) + "\n")
	case "EmptyQuery":
		sb.WriteString(labelStyle.Render("  • (empty query)") + "\n")
	}

	return sb.String()
}

// formatPostgresResultTable formats columns and rows as an aligned text table with NULL handling
func formatPostgresResultTable(columns []string, rows [][]string, nullFlags [][]bool) string {
	// Calculate column widths
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col)
	}
	for _, row := range rows {
		for i, val := range row {
			if i < len(widths) && len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Check NULLs for width calculation
	for ri, flags := range nullFlags {
		if ri >= len(rows) {
			break
		}
		for ci, isNull := range flags {
			if isNull && ci < len(widths) && 4 > widths[ci] { // "NULL" is 4 chars
				widths[ci] = 4
			}
		}
	}

	// Cap column widths
	for i := range widths {
		if widths[i] > 30 {
			widths[i] = 30
		}
	}

	var sb strings.Builder
	pad := "  "

	// Header
	sb.WriteString(pad)
	for i, col := range columns {
		if i > 0 {
			sb.WriteString(labelStyle.Render(" │ "))
		}
		sb.WriteString(subsectionStyle.Render(padRight(col, widths[i])))
	}
	sb.WriteString("\n")

	// Separator
	sb.WriteString(pad)
	for i, w := range widths {
		if i > 0 {
			sb.WriteString(labelStyle.Render("─┼─"))
		}
		sb.WriteString(labelStyle.Render(strings.Repeat("─", w)))
	}
	sb.WriteString("\n")

	// Rows
	for ri, row := range rows {
		sb.WriteString(pad)
		for ci := range columns {
			if ci > 0 {
				sb.WriteString(labelStyle.Render(" │ "))
			}
			val := ""
			isNull := false
			if ri < len(nullFlags) && ci < len(nullFlags[ri]) {
				isNull = nullFlags[ri][ci]
			}
			if isNull {
				val = "NULL"
				sb.WriteString(labelStyle.Render(padRight(val, widths[ci])))
			} else {
				if ci < len(row) {
					val = row[ci]
				}
				if len(val) > 30 {
					val = val[:27] + "..."
				}
				sb.WriteString(valueStyle.Render(padRight(val, widths[ci])))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// getPostgresStatusColor returns lipgloss color based on PostgreSQL result
func getPostgresStatusColor(res *plugins.PostgresResult) lipgloss.Color {
	if res == nil {
		return lipgloss.Color("7") // White - no result yet
	}
	if res.Type == "Error" {
		return lipgloss.Color("1") // Red
	}
	return lipgloss.Color("2") // Green
}

func getPostgresColorFn(res *plugins.PostgresResult) func(a ...any) string {
	if res == nil {
		return color.New(color.FgWhite).SprintFunc()
	}
	if res.Type == "Error" {
		return color.New(color.FgRed).SprintFunc()
	}
	return color.New(color.FgGreen).SprintFunc()
}
