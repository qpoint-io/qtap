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

// kafkaFilterInstance handles Kafka traffic for access logging
type kafkaFilterInstance struct {
	logger *zap.Logger
	writer *zap.Logger

	ctx    plugins.PluginContext
	mode   displayMode
	format outputFormat

	commands []*plugins.KafkaCommand
	results  []*plugins.KafkaResult
}

func (f *kafkaFilterInstance) OnKafkaCommand(cmd *plugins.KafkaCommand) plugins.KafkaStatus {
	f.commands = append(f.commands, cmd)
	return plugins.KafkaStatusContinue
}

func (f *kafkaFilterInstance) OnKafkaResult(res *plugins.KafkaResult) plugins.KafkaStatus {
	f.results = append(f.results, res)
	return plugins.KafkaStatusContinue
}

func (f *kafkaFilterInstance) Destroy() {
	if f.mode == displayModeNone {
		return
	}

	// Print each command/result pair
	for i, cmd := range f.commands {
		var res *plugins.KafkaResult
		if i < len(f.results) {
			res = f.results[i]
		}

		printer := NewKafkaPrinter(f.format, f.ctx, cmd, res, f.logger, f.writer)
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

// KafkaPrinter defines the interface for Kafka access log formatting
type KafkaPrinter interface {
	PrintSummary()
	PrintDetails()
}

// NewKafkaPrinter creates a new Kafka printer based on the specified format
func NewKafkaPrinter(
	format outputFormat,
	ctx plugins.PluginContext,
	cmd *plugins.KafkaCommand,
	res *plugins.KafkaResult,
	logger *zap.Logger,
	writer *zap.Logger,
) KafkaPrinter {
	switch format {
	case outputFormatJSON:
		return &kafkaJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	case outputFormatConsole:
		return &kafkaConsolePrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	default:
		return &kafkaJSONPrinter{ctx: ctx, cmd: cmd, res: res, logger: logger, writer: writer}
	}
}

// kafkaJSONPrinter implements KafkaPrinter for JSON format
type kafkaJSONPrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.KafkaCommand
	res    *plugins.KafkaResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *kafkaJSONPrinter) PrintSummary() {
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
		zap.String("operation", p.cmd.Operation),
		zap.Strings("topics", p.cmd.Topics),
		zap.String("request_id", requestID),
	}

	if p.cmd.ClientID != "" {
		fields = append(fields, zap.String("client_id", p.cmd.ClientID))
	}
	if p.cmd.GroupID != "" {
		fields = append(fields, zap.String("group_id", p.cmd.GroupID))
	}

	if p.res != nil {
		fields = append(fields, zap.Bool("is_error", p.res.IsError))
		if p.res.IsError {
			fields = append(fields, zap.Int16("error_code", p.res.ErrorCode))
		}
	}

	if len(p.cmd.Messages) > 0 {
		fields = append(fields, zap.Int("message_count", len(p.cmd.Messages)))
	}

	p.writer.Info("Kafka transaction", fields...)
}

func (p *kafkaJSONPrinter) PrintDetails() {
	meta := p.ctx.Meta()
	requestID := meta.RequestID()
	direction := meta.Direction()

	command := map[string]any{
		"operation":   p.cmd.Operation,
		"api_key":     p.cmd.ApiKey,
		"api_version": p.cmd.ApiVersion,
		"topics":      p.cmd.Topics,
		"timestamp":   p.cmd.Timestamp,
	}
	if p.cmd.ClientID != "" {
		command["client_id"] = p.cmd.ClientID
	}
	if p.cmd.GroupID != "" {
		command["group_id"] = p.cmd.GroupID
	}
	if len(p.cmd.Messages) > 0 {
		msgs := make([]map[string]any, 0, len(p.cmd.Messages))
		for _, m := range p.cmd.Messages {
			msgs = append(msgs, map[string]any{
				"topic":     m.Topic,
				"partition": m.Partition,
				"key":       m.Key,
				"value":     m.Value,
				"truncated": m.Truncated,
			})
		}
		command["messages"] = msgs
	}

	var result map[string]any
	if p.res != nil {
		result = map[string]any{
			"is_error":       p.res.IsError,
			"correlation_id": p.res.CorrelationID,
		}
		if p.res.IsError {
			result["error_code"] = p.res.ErrorCode
			result["error_message"] = p.res.ErrorMessage
		}
		if len(p.res.Messages) > 0 {
			msgs := make([]map[string]any, 0, len(p.res.Messages))
			for _, m := range p.res.Messages {
				msgs = append(msgs, map[string]any{
					"topic":     m.Topic,
					"partition": m.Partition,
					"key":       m.Key,
					"value":     m.Value,
					"truncated": m.Truncated,
				})
			}
			result["messages"] = msgs
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

	p.writer.Info("Kafka transaction", fields...)
}

// kafkaConsolePrinter implements KafkaPrinter for console format
type kafkaConsolePrinter struct {
	ctx    plugins.PluginContext
	cmd    *plugins.KafkaCommand
	res    *plugins.KafkaResult
	logger *zap.Logger
	writer *zap.Logger
}

func (p *kafkaConsolePrinter) PrintSummary() {
	meta := p.ctx.Meta()
	direction := meta.Direction()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	summaryLine := " " + buildKafkaSummary(bin, direction, p.cmd, p.res)
	fmt.Fprintln(accessLogsWriter, summaryLine)
}

func (p *kafkaConsolePrinter) PrintDetails() {
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

	borderColor := getKafkaStatusColor(p.res)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	leftBorder := borderStyle.Render("│")
	paddedLeftBorder := leftBorder + "  "

	var output strings.Builder

	// Top border
	output.WriteString(borderStyle.Render("╭"+strings.Repeat("─", borderWidth)+"┄") + "\n")

	// Summary
	output.WriteString(prefixLines(buildKafkaSummary(bin, direction, p.cmd, p.res), paddedLeftBorder) + "\n")

	// Meta section
	output.WriteString(buildSectionDivider(borderColor, "", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMetaContent(meta), paddedLeftBorder) + "\n")

	// Operation section
	output.WriteString(buildSectionDivider(borderColor, "OPERATION", borderWidth) + "\n")
	output.WriteString(prefixLines(buildKafkaOperationContent(p.cmd), paddedLeftBorder) + "\n")

	// Messages section (from command or result)
	messages := p.cmd.Messages
	msgTitle := "MESSAGES (PRODUCED)"
	if len(messages) == 0 && p.res != nil && len(p.res.Messages) > 0 {
		messages = p.res.Messages
		msgTitle = "MESSAGES (FETCHED)"
	}
	if len(messages) > 0 {
		output.WriteString(buildSectionDivider(borderColor, msgTitle, borderWidth) + "\n")
		output.WriteString(prefixLines(buildKafkaMessagesContent(messages), paddedLeftBorder) + "\n")
	}

	// Result section
	if p.res != nil {
		resultTitle := "RESULT"
		if p.res.IsError {
			resultTitle = "RESULT (ERROR)"
		}
		output.WriteString(buildSectionDivider(borderColor, resultTitle, borderWidth) + "\n")
		output.WriteString(prefixLines(buildKafkaResultContent(p.res), paddedLeftBorder) + "\n")
	}

	// Bottom border
	output.WriteString(borderStyle.Render("╰" + strings.Repeat("─", borderWidth) + "┄"))

	fmt.Fprintln(accessLogsWriter, output.String())
}

// buildKafkaSummary creates the summary line for Kafka commands
func buildKafkaSummary(cmd, direction string, kafkaCmd *plugins.KafkaCommand, res *plugins.KafkaResult) string {
	fn := getKafkaColorFn(res)
	shortCmd := filepath.Base(cmd)

	arrow := "→"
	if !strings.Contains(direction, "egress") {
		arrow = "←"
	}
	arrow = fn(arrow)

	operation := lipgloss.NewStyle().Bold(true).Render(kafkaCmd.Operation)

	// Show topics
	var topicInfo string
	if len(kafkaCmd.Topics) > 0 {
		topicInfo = `"` + strings.Join(kafkaCmd.Topics, ", ") + `"`
		if len(topicInfo) > 50 {
			topicInfo = topicInfo[:47] + `..."`
		}
	}

	// Show message count for produce/fetch
	var msgCount string
	if len(kafkaCmd.Messages) > 0 {
		msgCount = strconv.Itoa(len(kafkaCmd.Messages)) + " msgs"
	} else if res != nil && len(res.Messages) > 0 {
		msgCount = strconv.Itoa(len(res.Messages)) + " msgs"
	}

	var status string
	if res != nil {
		if res.IsError {
			status = fn("ERR")
		} else {
			status = fn("OK")
		}
	}

	parts := []string{fn("■"), shortCmd, arrow, operation}
	if topicInfo != "" {
		parts = append(parts, topicInfo)
	}
	if msgCount != "" {
		parts = append(parts, valueStyle.Render(msgCount))
	}
	if status != "" {
		parts = append(parts, status)
	}

	return strings.Join(parts, " ")
}

func buildKafkaOperationContent(cmd *plugins.KafkaCommand) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Operation") + "\n")
	sb.WriteString(labelStyle.Render("  • Type: ") + valueStyle.Render(cmd.Operation) + "\n")
	sb.WriteString(labelStyle.Render("  • API Key: ") + valueStyle.Render(strconv.Itoa(int(cmd.ApiKey))) + "\n")
	sb.WriteString(labelStyle.Render("  • API Version: ") + valueStyle.Render(strconv.Itoa(int(cmd.ApiVersion))) + "\n")

	if cmd.ClientID != "" {
		sb.WriteString(labelStyle.Render("  • Client ID: ") + valueStyle.Render(cmd.ClientID) + "\n")
	}
	if cmd.GroupID != "" {
		sb.WriteString(labelStyle.Render("  • Group ID: ") + valueStyle.Render(cmd.GroupID) + "\n")
	}
	if len(cmd.Topics) > 0 {
		sb.WriteString(labelStyle.Render("  • Topics: ") + valueStyle.Render(strings.Join(cmd.Topics, ", ")) + "\n")
	}
	sb.WriteString(labelStyle.Render("  • Time: ") + valueStyle.Render(cmd.Timestamp.Format("15:04:05.000")) + "\n")

	return sb.String()
}

func buildKafkaMessagesContent(messages []plugins.KafkaMessage) string {
	var sb strings.Builder
	sb.WriteString("\n")

	for i, msg := range messages {
		if i >= 10 { // Show at most 10 messages
			sb.WriteString(labelStyle.Render(fmt.Sprintf("  ... and %d more", len(messages)-10)) + "\n")
			break
		}

		header := fmt.Sprintf("Message %d", i+1)
		if msg.Topic != "" {
			header += " [" + msg.Topic + "]"
		}
		sb.WriteString(subsectionStyle.Render("  "+header) + "\n")

		if msg.Key != "" {
			key := msg.Key
			if len(key) > 50 {
				key = key[:47] + "..."
			}
			sb.WriteString(labelStyle.Render("    • Key: ") + valueStyle.Render(key) + "\n")
		}

		val := msg.Value
		if len(val) > 100 {
			val = val[:97] + "..."
		}
		if val != "" {
			sb.WriteString(labelStyle.Render("    • Value: ") + valueStyle.Render(val) + "\n")
		}

		if msg.Truncated {
			sb.WriteString(labelStyle.Render("    • (value truncated)") + "\n")
		}
	}

	return sb.String()
}

func buildKafkaResultContent(res *plugins.KafkaResult) string {
	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString(subsectionStyle.Render("Result") + "\n")
	if res.IsError {
		sb.WriteString(labelStyle.Render("  • Error Code: ") + valueStyle.Render(strconv.Itoa(int(res.ErrorCode))) + "\n")
		sb.WriteString(labelStyle.Render("  • Error: ") + valueStyle.Render(res.ErrorMessage) + "\n")
	} else {
		sb.WriteString(labelStyle.Render("  • Status: ") + valueStyle.Render("OK") + "\n")
	}
	sb.WriteString(labelStyle.Render("  • Correlation ID: ") + valueStyle.Render(strconv.Itoa(int(res.CorrelationID))) + "\n")

	return sb.String()
}

// getKafkaStatusColor returns lipgloss color based on Kafka result
func getKafkaStatusColor(res *plugins.KafkaResult) lipgloss.Color {
	if res == nil {
		return lipgloss.Color("7") // White
	}
	if res.IsError {
		return lipgloss.Color("1") // Red
	}
	return lipgloss.Color("2") // Green
}

func getKafkaColorFn(res *plugins.KafkaResult) func(a ...interface{}) string {
	if res == nil {
		return color.New(color.FgWhite).SprintFunc()
	}
	if res.IsError {
		return color.New(color.FgRed).SprintFunc()
	}
	return color.New(color.FgGreen).SprintFunc()
}
