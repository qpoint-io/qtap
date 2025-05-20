package accesslogs

import (
	"errors"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"

	"github.com/qpoint-io/rulekit"
	"go.uber.org/zap"
)

type displayMode string

const (
	displayModeNone    displayMode = "none"
	displayModeSummary displayMode = "summary"
	displayModeDetails displayMode = "details"
	displayModeFull    displayMode = "full"
)

type outputFormat string

const (
	outputFormatJSON    outputFormat = "json"
	outputFormatConsole outputFormat = "console"
)

type logRule struct {
	Name string      `yaml:"name"`
	Expr string      `yaml:"expr"`
	Mode displayMode `yaml:"mode"`

	rule rulekit.Rule `yaml:"-"`
}

type filterInstance struct {
	logger *zap.Logger
	writer *zap.Logger
	ctx    plugins.PluginContext

	mode   displayMode
	format outputFormat
	rules  []logRule

	reqheaders plugins.Headers
	resheaders plugins.Headers
}

func (f *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	f.reqheaders = headers
	return plugins.HeadersStatusContinue
}

func (f *filterInstance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if !endOfStream {
		return plugins.BodyStatusStopIterationAndBuffer
	}

	return plugins.BodyStatusContinue
}

func (f *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	f.resheaders = headers
	return plugins.HeadersStatusContinue
}

func (f *filterInstance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if !endOfStream {
		return plugins.BodyStatusStopIterationAndBuffer
	}

	return plugins.BodyStatusContinue
}

func (f *filterInstance) Destroy() {
	mode := f.mode

	reqPairs := tools.NewHeaderMap(f.reqheaders).RulePairs("request")
	resPairs := tools.NewHeaderMap(f.resheaders).RulePairs("response")

	var metaPairs map[string]any
	if f.ctx != nil {
		metaPairs = tools.MetadataRulePairs(f.ctx.Metadata())
	} else {
		metaPairs = make(map[string]any)
	}

	allPairs := make(map[string]any)
	for k, v := range reqPairs {
		allPairs[k] = v
	}
	for k, v := range resPairs {
		allPairs[k] = v
	}
	for k, v := range metaPairs {
		allPairs[k] = v
	}

	for _, r := range f.rules {
		if r.rule == nil {
			continue
		}

		res := r.rule.Eval(&rulekit.Ctx{
			KV: allPairs,
		})
		if !res.Ok() {
			// log any non-ErrMissingFields errors
			mf := &rulekit.ErrMissingFields{}
			if !errors.As(res.Error, &mf) {
				f.logger.Error("error evaluating rule",
					zap.Error(res.Error),
					zap.String("evaluated_rule", res.EvaluatedRule.String()),
				)
			}
			continue
		}
		if res.Pass() {
			mode = r.Mode
			break
		}
	}

	if mode == displayModeNone {
		return
	}

	// Create the appropriate logger based on the format
	logger := NewPrinter(
		f.format,
		f.ctx,
		f.reqheaders,
		f.resheaders,
		f.logger,
		f.writer,
	)

	// Call the appropriate method based on the mode
	switch mode {
	case displayModeSummary:
		logger.PrintSummary()
	case displayModeDetails:
		if err := logger.PrintDetails(); err != nil {
			f.logger.Error("failed to print details", zap.Error(err))
		}
	case displayModeFull:
		if err := logger.PrintFull(); err != nil {
			f.logger.Error("failed to print full details", zap.Error(err))
		}
	default:
		logger.PrintSummary()
	}
}
