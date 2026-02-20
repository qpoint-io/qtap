package trace

import (
	"fmt"
	"strings"
)

type Toggle struct {
	Key   string
	Value string
}

type Matcher struct {
	toggles []*Toggle
}

func NewMatcher(toggleQuery string) (*Matcher, error) {
	matcher := &Matcher{}
	if toggleQuery == "" {
		return matcher, nil
	}

	toggles := strings.SplitSeq(toggleQuery, ",")
	for toggle := range toggles {
		parts := strings.Split(toggle, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid toggle format: %s", toggle)
		}

		matcher.toggles = append(matcher.toggles, &Toggle{
			Key:   parts[0],
			Value: parts[1],
		})
	}

	return matcher, nil
}

func (m *Matcher) MatchExe(exe string) bool {
	for _, t := range m.toggles {
		switch t.Key {
		case "exe":
			if exe == t.Value || t.Value == "*" {
				return true
			}
		case "exe.contains":
			if strings.Contains(exe, t.Value) {
				return true
			}
		case "exe.startsWith":
			if strings.HasPrefix(exe, t.Value) {
				return true
			}
		case "exe.endsWith":
			if strings.HasSuffix(exe, t.Value) {
				return true
			}
		}
	}
	return false
}

func (m *Matcher) HasProcToggles() bool {
	for _, t := range m.toggles {
		if strings.HasPrefix(t.Key, "exe") {
			return true
		}
	}
	return false
}

func (m *Matcher) GetModuleToggles() []string {
	var modules []string
	for _, t := range m.toggles {
		if t.Key == "mod" {
			modules = append(modules, t.Value)
		}
	}
	return modules
}
