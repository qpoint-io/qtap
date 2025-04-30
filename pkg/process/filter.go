package process

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/qpoint-io/qtap/pkg/config"
	"go.uber.org/zap"
)

type Filter interface {
	Evaluate(*Process) (bool, error)
	Bitmask() uint8
}

var filters []Filter
var filterMu sync.RWMutex

func applyFilters(p *Process) uint8 {
	filterMu.RLock()
	defer filterMu.RUnlock()

	for _, filter := range filters {
		match, err := filter.Evaluate(p)
		if err != nil {
			zap.L().Error("error evaluating filter, continuing",
				zap.String("exe", p.Exe),
				zap.Any("filter", filter),
				zap.Error(err))
			continue
		}

		if match {
			return filter.Bitmask()
		}
	}

	return 0
}

func (m *Manager) updateFilters(cfg *config.Config) {
	if cfg.Tap == nil {
		return
	}

	var f []Filter

	// add custom filter
	for _, filter := range cfg.Tap.Filters.Custom {
		if err := filter.Validate(); err != nil {
			m.Logger.Warn("invalid filter; not loading", zap.Any("filter", filter), zap.Error(err))
			continue
		}

		filter, err := FromConfigFilter(&filter)
		if err != nil {
			m.Logger.Warn("invalid filter; not loading", zap.Any("filter", filter), zap.Error(err))
			continue
		}

		f = append(f, filter)
	}

	// add predefined filters from groups
	for _, group := range cfg.Tap.Filters.Groups {
		f = append(f, getPredefinedFilters(group)...)
	}

	// set the filters
	filterMu.Lock()
	filters = f
	filterMu.Unlock()

	// debug
	m.Logger.Debug("applying filters to existing processes", zap.Any("filters", filters))

	// check existing exe map for existing pids and set the flags accordingly
	m.procs.Iter(func(pid int, p *Process) bool {
		if f := applyFilters(p); f != 0 {
			// add to the bpf map
			p.filter = f
			if p.notifier != nil {
				if err := p.notifier(); err != nil {
					m.Logger.Warn("failed to notify eventer",
						zap.Int("pid", pid),
						zap.String("exe", p.Exe),
						zap.Any("flag", f),
						zap.Error(err))
				}
			}

			// debug
			m.Logger.Debug("filtering existing process",
				zap.Int("pid", pid),
				zap.String("exe", p.Exe),
				zap.String("flag", fmt.Sprintf("%08b", f)))
		}

		// continue iterating
		return true
	})
}

func FromConfigFilter(filter *config.TapFilter) (Filter, error) {
	if filter.Strategy == config.MatchStrategy_REGEX {
		re, err := regexp.Compile(filter.Exe)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		return &ExeRegexFilter{pattern: re, bitmask: filter.Pack()}, nil
	}

	return &ExeFilter{
		pattern:  filter.Exe,
		strategy: filter.Strategy,
		bitmask:  filter.Pack(),
	}, nil
}

type ExeFilter struct {
	pattern  string
	strategy config.MatchStrategy
	bitmask  uint8
}

func (f *ExeFilter) Evaluate(p *Process) (bool, error) {
	if p.Exe == "" || f.pattern == "" {
		return false, nil
	}

	input := strings.ToLower(p.Exe)
	exe := strings.ToLower(f.pattern)

	strategy := f.strategy
	if strategy == "" {
		// default to suffix if unspecified
		strategy = config.MatchStrategy_SUFFIX
	}

	switch strategy {
	case config.MatchStrategy_EXACT:
		return exe == input, nil
	case config.MatchStrategy_PREFIX:
		return strings.HasPrefix(input, exe), nil
	case config.MatchStrategy_SUFFIX:
		return strings.HasSuffix(input, exe), nil
	case config.MatchStrategy_CONTAINS:
		return strings.Contains(input, exe), nil
	default:
		return false, fmt.Errorf("invalid strategy: %s", f.strategy)
	}
}

func (f *ExeFilter) Bitmask() uint8 {
	return f.bitmask
}

type ExeRegexFilter struct {
	pattern *regexp.Regexp
	bitmask uint8
}

func (f *ExeRegexFilter) Evaluate(p *Process) (bool, error) {
	return f.pattern.MatchString(p.Exe), nil
}

func (f *ExeRegexFilter) Bitmask() uint8 {
	return f.bitmask
}

type PIDFilter struct {
	PID     int
	bitmask uint8
}

func (f *PIDFilter) Evaluate(p *Process) (bool, error) {
	return f.PID == p.Pid, nil
}

func (f *PIDFilter) Bitmask() uint8 {
	return f.bitmask
}
