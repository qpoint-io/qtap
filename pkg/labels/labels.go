package labels

import (
	"strings"

	"github.com/qpoint-io/rulekit/set"
)

type Set set.Set[string]

func New(items ...string) Set {
	s := make(Set)
	s.Add(items...)
	return s
}

func (s Set) Add(items ...string) {
	for _, item := range items {
		s[format(item)] = struct{}{}
	}
}

func (s Set) Items() []string {
	return set.Set[string](s).Items()
}

func format(item string) string {
	item = strings.ToLower(item)
	item = strings.TrimSpace(item)
	item = strings.ReplaceAll(item, " ", "-")
	return item
}
