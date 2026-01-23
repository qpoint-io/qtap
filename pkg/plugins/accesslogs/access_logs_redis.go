package accesslogs

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"go.uber.org/zap"
)

// redisFilterInstance handles Redis traffic for access logging
type redisFilterInstance struct {
	logger *zap.Logger
	writer *zap.Logger

	ctx    plugins.PluginContext
	mode   displayMode
	format outputFormat

	commands []*plugins.RedisCommand
	results  []*plugins.RedisResult
}

func (f *redisFilterInstance) OnRedisCommand(cmd *plugins.RedisCommand) plugins.RedisStatus {
	f.commands = append(f.commands, cmd)
	return plugins.RedisStatusContinue
}

func (f *redisFilterInstance) OnRedisResult(res *plugins.RedisResult) plugins.RedisStatus {
	f.results = append(f.results, res)
	return plugins.RedisStatusContinue
}

func (f *redisFilterInstance) Destroy() {
	if f.mode == displayModeNone {
		return
	}

	// Print each command/result pair
	for i, cmd := range f.commands {
		var res *plugins.RedisResult
		if i < len(f.results) {
			res = f.results[i]
		}

		printer := NewRedisPrinter(f.format, f.ctx, cmd, res, f.logger, f.writer)
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

// RedisPrinter defines the interface for Redis access log formatting
type RedisPrinter interface {
	PrintSummary()
	PrintDetails()
}

// NewRedisPrinter creates a new Redis printer based on the specified format
func NewRedisPrinter(
	format outputFormat,
	ctx plugins.PluginContext,
	cmd *plugins.RedisCommand,
	res *plugins.RedisResult,
	logger *zap.Logger,
	writer *zap.Logger,
) RedisPrinter {
	switch format {
	case outputFormatJSON:
		return &redisJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	case outputFormatConsole:
		return &redisConsolePrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	default:
		return &redisJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	}
}

// redisJSONPrinter implements RedisPrinter for JSON format
type redisJSONPrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.RedisCommand
	res    *plugins.RedisResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *redisJSONPrinter) PrintSummary() {
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
		zap.String("command", p.cmd.Name),
		zap.Strings("args", p.cmd.Args),
		zap.String("request_id", requestID),
	}

	if p.res != nil {
		fields = append(fields,
			zap.String("result_type", p.res.Type),
			zap.Bool("is_error", p.res.IsError),
		)
	}

	p.writer.Info("Redis transaction", fields...)
}

func (p *redisJSONPrinter) PrintDetails() {
	meta := p.ctx.Meta()
	requestID := meta.RequestID()
	direction := meta.Direction()

	command := map[string]any{
		"name":      p.cmd.Name,
		"args":      p.cmd.Args,
		"timestamp": p.cmd.Timestamp,
	}

	var result map[string]any
	if p.res != nil {
		result = map[string]any{
			"type":     p.res.Type,
			"value":    p.res.Value,
			"is_error": p.res.IsError,
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

	p.writer.Info("Redis transaction", fields...)
}

// redisConsolePrinter implements RedisPrinter for console format
type redisConsolePrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.RedisCommand
	res    *plugins.RedisResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *redisConsolePrinter) PrintSummary() {
	meta := p.ctx.Meta()
	direction := meta.Direction()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	summaryLine := " " + buildRedisSummary(bin, direction, p.cmd, p.res)
	fmt.Fprintln(accessLogsWriter, summaryLine)
}

func (p *redisConsolePrinter) PrintDetails() {
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

	borderColor := getRedisStatusColor(p.res)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	leftBorder := borderStyle.Render("│")
	paddedLeftBorder := leftBorder + "  "

	var output strings.Builder

	// Top border
	output.WriteString(borderStyle.Render("╭"+strings.Repeat("─", borderWidth)+"┄") + "\n")

	// Summary
	output.WriteString(prefixLines(buildRedisSummary(bin, direction, p.cmd, p.res), paddedLeftBorder) + "\n")

	// Meta section
	output.WriteString(buildSectionDivider(borderColor, "", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMetaContent(meta), paddedLeftBorder) + "\n")

	// Command section
	output.WriteString(buildSectionDivider(borderColor, "COMMAND", borderWidth) + "\n")
	output.WriteString(prefixLines(buildRedisCommandContent(p.cmd), paddedLeftBorder) + "\n")

	// Result section
	if p.res != nil {
		resultTitle := "RESULT"
		if p.res.IsError {
			resultTitle = "RESULT (ERROR)"
		}
		output.WriteString(buildSectionDivider(borderColor, resultTitle, borderWidth) + "\n")
		output.WriteString(prefixLines(buildRedisResultContent(p.res), paddedLeftBorder) + "\n")
	}

	// Bottom border
	output.WriteString(borderStyle.Render("╰" + strings.Repeat("─", borderWidth) + "┄"))

	fmt.Fprintln(accessLogsWriter, output.String())
}

// buildRedisSummary creates the summary line for Redis commands
func buildRedisSummary(cmd, direction string, redisCmd *plugins.RedisCommand, res *plugins.RedisResult) string {
	fn := getRedisColorFn(res)
	shortCmd := filepath.Base(cmd)

	arrow := "→"
	if !strings.Contains(direction, "egress") {
		arrow = "←"
	}
	arrow = fn(arrow)

	cmdName := lipgloss.NewStyle().Bold(true).Render(redisCmd.Name)
	args := strings.Join(redisCmd.Args, " ")
	if len(args) > 50 {
		args = args[:47] + "..."
	}

	var status string
	if res != nil {
		if res.IsError {
			status = fn("ERR")
		} else {
			status = fn("OK")
		}
	}

	summary := fmt.Sprintf("%s %s %s %s %s %s",
		fn("■"), shortCmd, arrow, cmdName, args, status)

	return summary
}

func buildRedisCommandContent(cmd *plugins.RedisCommand) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Command") + "\n")
	sb.WriteString(labelStyle.Render("  • Name: ") + valueStyle.Render(cmd.Name) + "\n")

	if len(cmd.Args) > 0 {
		sb.WriteString(labelStyle.Render("  • Args: ") + valueStyle.Render(strings.Join(cmd.Args, " ")) + "\n")
	}

	sb.WriteString(labelStyle.Render("  • Time: ") + valueStyle.Render(cmd.Timestamp.Format("15:04:05.000")) + "\n")

	return sb.String()
}

func buildRedisResultContent(res *plugins.RedisResult) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Result") + "\n")
	sb.WriteString(labelStyle.Render("  • Type: ") + valueStyle.Render(res.Type) + "\n")

	valueStr := fmt.Sprintf("%v", res.Value)
	if len(valueStr) > 100 {
		valueStr = valueStr[:97] + "..."
	}
	sb.WriteString(labelStyle.Render("  • Value: ") + valueStyle.Render(valueStr) + "\n")

	return sb.String()
}

// getRedisStatusColor returns lipgloss color based on Redis result
func getRedisStatusColor(res *plugins.RedisResult) lipgloss.Color {
	if res == nil {
		return lipgloss.Color("7") // White - no result yet
	}
	if res.IsError {
		return lipgloss.Color("1") // Red
	}
	return lipgloss.Color("2") // Green
}

func getRedisColorFn(res *plugins.RedisResult) func(a ...interface{}) string {
	if res == nil {
		return color.New(color.FgWhite).SprintFunc()
	}
	if res.IsError {
		return color.New(color.FgRed).SprintFunc()
	}
	return color.New(color.FgGreen).SprintFunc()
}
