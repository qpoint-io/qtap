package providers

import (
	"strings"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
)

type Provider interface {
	Name() string

	// provider detection
	Hosts() []StringMatcher
	RequestHeaders() []HeaderMatcher
	ResponseHeaders() []HeaderMatcher

	// conversation detection
	Paths() []StringMatcher
	Handle(
		path string,
		reqHeaders plugins.Headers, reqBody []byte,
		resHeaders plugins.Headers, resBody []byte,
	) ([]*conversations.Completion, error)
}

type StringMatcher func(string) bool

func StringExact(s string) StringMatcher {
	return func(v string) bool {
		return v == s
	}
}

func StringSuffix(s string) StringMatcher {
	return func(v string) bool {
		return strings.HasSuffix(v, s)
	}
}

func StringPrefix(s string) StringMatcher {
	return func(v string) bool {
		return strings.HasPrefix(v, s)
	}
}

type HeaderMatcher struct {
	Name  string
	Value StringMatcher
}

func (h HeaderMatcher) MatchValue(value string) bool {
	if h.Value == nil {
		return true
	}

	return h.Value(value)
}
