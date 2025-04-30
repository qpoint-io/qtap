package process

import (
	"errors"
	"strings"

	"github.com/qpoint-io/qtap/pkg/config"
)

const (
	QpointStrategyEnvVar = "QPOINT_STRATEGY"
	QpointTagsEnvVar     = "QPOINT_TAGS"
)

// QpointStrategy represents the different qpoint strategies that can be used.
type QpointStrategy uint32

const (
	StrategyObserve QpointStrategy = iota
	StrategyIgnore
	StrategyAudit
	StrategyForward
	StrategyProxy
)

// createTapFilter parses filter strings and creates a TapFilter
func createTapFilter(filterStr string) (*config.TapFilter, error) {
	filterStr, _ = strings.CutPrefix(filterStr, "exe.")

	strategy, matchValue, found := strings.Cut(filterStr, ":")
	if !found {
		return nil, errors.New("invalid filter format")
	}

	var matchStrategy config.MatchStrategy
	if !matchStrategy.Parse(strategy) {
		return nil, errors.New("invalid match strategy")
	}

	return &config.TapFilter{
		Exe:      matchValue,
		Strategy: matchStrategy,
	}, nil
}

func QpointStrategyFromString(s string, p *Process) (QpointStrategy, error) {
	strat, filterStr, found := strings.Cut(s, ",")
	if found {
		var match bool
		filterStrs := strings.Split(filterStr, ",")
		for _, filterStr := range filterStrs {
			filterConfig, err := createTapFilter(filterStr)
			if err != nil {
				return StrategyObserve, err
			}

			filter, err := FromConfigFilter(filterConfig)
			if err != nil {
				return StrategyObserve, err
			}

			// evaluate the filter
			match, err = filter.Evaluate(p)
			if err != nil {
				return StrategyObserve, err
			}

			if !match {
				continue
			}

			// we found a match
			match = true
			break
		}

		if !match {
			return StrategyObserve, nil
		}
	}

	switch strat {
	case "observe":
		return StrategyObserve, nil
	case "ignore":
		return StrategyIgnore, nil
	case "audit":
		return StrategyAudit, nil
	case "forward":
		return StrategyForward, nil
	case "proxy":
		return StrategyProxy, nil
	default:
		return StrategyObserve, nil
	}
}

func (s QpointStrategy) String() string {
	switch s {
	case StrategyObserve:
		return "observe"
	case StrategyIgnore:
		return "ignore"
	case StrategyAudit:
		return "audit"
	case StrategyForward:
		return "forward"
	case StrategyProxy:
		return "proxy"
	default:
		return "observe"
	}
}
