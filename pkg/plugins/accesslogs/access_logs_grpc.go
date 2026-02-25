package accesslogs

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"go.uber.org/zap"
)

// grpcFilterInstance embeds filterInstance to reuse all HTTP plugin hook methods.
// Only Destroy() is overridden to log gRPC-specific metadata.
type grpcFilterInstance struct {
	filterInstance
}

func (f *grpcFilterInstance) Destroy() {
	if f.mode == displayModeNone {
		return
	}

	hm := tools.NewHeaderMap(f.reqheaders)
	rhm := tools.NewHeaderMap(f.resheaders)

	grpcService, grpcMethod := hm.GRPCServiceMethod()
	grpcStatus, _ := rhm.Get("Grpc-Status")
	grpcStatusName, _ := rhm.Get("Grpc-Status-Name")
	grpcMessage, _ := rhm.Get("Grpc-Message")

	printer := NewGrpcPrinter(f.format, f.ctx, grpcService, grpcMethod, grpcStatus, grpcStatusName, grpcMessage, f.logger, f.writer)
	switch f.mode {
	case displayModeDetails, displayModeFull:
		printer.PrintDetails()
	default:
		printer.PrintSummary()
	}
}

// GrpcPrinter defines the interface for gRPC access log formatting.
type GrpcPrinter interface {
	PrintSummary()
	PrintDetails()
}

// NewGrpcPrinter creates a new gRPC printer based on the specified format.
func NewGrpcPrinter(
	format outputFormat,
	ctx plugins.PluginContext,
	service, method, status, statusName, message string,
	logger *zap.Logger,
	writer *zap.Logger,
) GrpcPrinter {
	switch format {
	case outputFormatJSON:
		return &grpcJSONPrinter{ctx: ctx, service: service, method: method, status: status, statusName: statusName, message: message, logger: logger, writer: writer}
	case outputFormatConsole:
		return &grpcConsolePrinter{ctx: ctx, service: service, method: method, status: status, statusName: statusName, message: message, logger: logger, writer: writer}
	default:
		return &grpcJSONPrinter{ctx: ctx, service: service, method: method, status: status, statusName: statusName, message: message, logger: logger, writer: writer}
	}
}

// ----- JSON printer -----

type grpcJSONPrinter struct {
	ctx        plugins.PluginContext
	service    string
	method     string
	status     string
	statusName string
	message    string
	logger     *zap.Logger
	writer     *zap.Logger
}

func (p *grpcJSONPrinter) PrintSummary() {
	meta := p.ctx.Meta()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	fields := []zap.Field{
		zap.String("bin", bin),
		zap.String("direction", meta.Direction()),
		zap.String("service", p.service),
		zap.String("method", p.method),
		zap.String("grpc_status", p.status),
		zap.String("grpc_status_name", p.statusName),
		zap.String("request_id", meta.RequestID()),
	}
	if p.message != "" {
		fields = append(fields, zap.String("grpc_message", p.message))
	}

	p.writer.Info("gRPC transaction", fields...)
}

func (p *grpcJSONPrinter) PrintDetails() {
	meta := p.ctx.Meta()

	rpc := map[string]any{
		"service":     p.service,
		"method":      p.method,
		"status":      p.status,
		"status_name": p.statusName,
		"request_id":  meta.RequestID(),
	}
	if p.message != "" {
		rpc["message"] = p.message
	}

	values := map[string]any{
		"bytes_sent":     meta.WriteBytes(),
		"bytes_received": meta.ReadBytes(),
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

	p.writer.Info("gRPC transaction",
		zap.Any("meta", values),
		zap.String("direction", meta.Direction()),
		zap.Any("rpc", rpc),
	)
}

// ----- Console printer -----

type grpcConsolePrinter struct {
	ctx        plugins.PluginContext
	service    string
	method     string
	status     string
	statusName string
	message    string
	logger     *zap.Logger
	writer     *zap.Logger
}

func (p *grpcConsolePrinter) PrintSummary() {
	meta := p.ctx.Meta()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	fmt.Fprintln(accessLogsWriter, " "+buildGRPCSummary(bin, meta.Direction(), p.service, p.method, p.statusName))
}

func (p *grpcConsolePrinter) PrintDetails() {
	meta := p.ctx.Meta()

	var bin string
	if proc := meta.Process(); proc != nil {
		bin = proc.Exe
	}

	termWidth := terminalWidth()
	borderWidth := termWidth - 2
	if borderWidth < 0 {
		borderWidth = 0
	} else if borderWidth > borderWidthCap {
		borderWidth = borderWidthCap
	}

	isError := p.status != "" && p.status != "0"
	borderColor := getGRPCStatusColor(isError)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	leftBorder := borderStyle.Render("│")
	paddedLeftBorder := leftBorder + "  "

	var output strings.Builder

	output.WriteString(borderStyle.Render("╭"+strings.Repeat("─", borderWidth)+"┄") + "\n")
	output.WriteString(prefixLines(buildGRPCSummary(bin, meta.Direction(), p.service, p.method, p.statusName), paddedLeftBorder) + "\n")
	output.WriteString(buildSectionDivider(borderColor, "", borderWidth) + "\n")
	output.WriteString(prefixLines(buildMetaContent(meta), paddedLeftBorder) + "\n")
	output.WriteString(buildSectionDivider(borderColor, "RPC", borderWidth) + "\n")
	output.WriteString(prefixLines(buildGRPCRPCContent(p.service, p.method, p.status, p.statusName, p.message), paddedLeftBorder) + "\n")
	output.WriteString(borderStyle.Render("╰" + strings.Repeat("─", borderWidth) + "┄"))

	fmt.Fprintln(accessLogsWriter, output.String())
}

func buildGRPCSummary(cmd, direction, service, method, statusName string) string {
	isError := statusName != "" && statusName != "OK"
	fn := getGRPCColorFn(isError)
	shortCmd := filepath.Base(cmd)

	arrow := "→"
	if !strings.Contains(direction, "egress") {
		arrow = "←"
	}

	rpc := lipgloss.NewStyle().Bold(true).Render(service + "/" + method)
	return fmt.Sprintf("%s %s %s %s %s", fn("■"), shortCmd, fn(arrow), rpc, fn(statusName))
}

func buildGRPCRPCContent(service, method, status, statusName, message string) string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(subsectionStyle.Render("RPC") + "\n")
	sb.WriteString(labelStyle.Render("  • Service: ") + valueStyle.Render(service) + "\n")
	sb.WriteString(labelStyle.Render("  • Method:  ") + valueStyle.Render(method) + "\n")
	sb.WriteString(labelStyle.Render("  • Status:  ") + valueStyle.Render(statusName) + labelStyle.Render(" ("+status+")") + "\n")
	if message != "" {
		sb.WriteString(labelStyle.Render("  • Message: ") + valueStyle.Render(message) + "\n")
	}
	return sb.String()
}

func getGRPCStatusColor(isError bool) lipgloss.Color {
	if isError {
		return lipgloss.Color("1") // Red
	}
	return lipgloss.Color("2") // Green
}

func getGRPCColorFn(isError bool) func(a ...any) string {
	if isError {
		return color.New(color.FgRed).SprintFunc()
	}
	return color.New(color.FgGreen).SprintFunc()
}
