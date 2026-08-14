package errordetection

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"go.uber.org/zap"
)

type HTTPMessage interface {
	Headers() *tools.HeaderMap
	Body() []byte
	Tags() []string
	Duration() time.Duration
}

type Matcher interface {
	Match(req, res HTTPMessage) (triggers []TriggerCondition, match bool)
}

type MatcherGroup []Matcher

func (g MatcherGroup) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	var triggers []TriggerCondition
	for _, m := range g {
		t, match := m.Match(req, res)
		if !match {
			return nil, false
		}
		triggers = append(triggers, t...)
	}
	return triggers, true
}

type mimeCategoryMatcher struct {
	logger     *zap.Logger
	categories []string
}

func (m *mimeCategoryMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	ct, ok := res.Headers().ContentType()
	if !ok {
		return nil, false
	}
	mc, ok := res.Headers().MimeCategory()
	if !ok {
		return nil, false
	}
	if slices.Contains(m.categories, mc) {
		return []TriggerCondition{{
			Condition: ConditionMIME,
			Reason:    fmt.Sprintf("response content-type [%s] matched category [%s]", ct, mc),
		}}, true
	}
	return nil, false
}

type statusCodeMatcher struct {
	logger *zap.Logger
	codes  []string
}

func (m *statusCodeMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	statusCode, ok := res.Headers().Status()
	status := strconv.Itoa(statusCode)
	if !ok || len(status) != 3 {
		return nil, false
	}

	for _, code := range m.codes {
		a, b, c := status[0], status[1], status[2]
		d, e, f := code[0], code[1], code[2]

		// look for a match, but 'x' is a wildcard, as in 5xx
		if a == d && (b == e || e == 'x') && (c == f || f == 'x') {
			m.logger.Debug("matched status code", zap.String("request_status", status), zap.String("matched_status", code))
			return []TriggerCondition{{
				Condition: ConditionStatus,
				Reason:    fmt.Sprintf("response status code [%s] matched [%s]", status, code),
			}}, true
		}
	}

	return nil, false
}

type urlMatcher struct {
	logger  *zap.Logger
	include []string
	exclude []string
}

func (m *urlMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	url, ok := req.Headers().URL()
	if !ok {
		return nil, false
	}

	var triggers []TriggerCondition

	// If URL is in exclude list, return false
	if slices.Contains(m.exclude, url) {
		m.logger.Debug("excluded url", zap.String("url", url))
		return nil, false
	}
	if len(m.exclude) > 0 {
		// an exclude list exists and the url is not in it
		triggers = append(triggers, TriggerCondition{
			Condition: ConditionURL,
			Reason:    "url not in exclude list",
		})
	}

	// If include list is empty, accept all non-excluded URLs
	if len(m.include) == 0 {
		return triggers, true
	}

	// If include list exists, URL must be in it
	if slices.Contains(m.include, url) {
		triggers = append(triggers, TriggerCondition{
			Condition: ConditionURL,
			Reason:    "url in include list",
		})
		return triggers, true
	}

	// url does not match either list
	return nil, false
}

type tagMatcher struct {
	logger *zap.Logger
	tags   []string
}

func (m *tagMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	reqTags := req.Tags()
	for _, tag := range m.tags {
		if slices.Contains(reqTags, tag) {
			m.logger.Debug("matched tag", zap.String("tag", tag))
			return []TriggerCondition{{
				Condition: ConditionTag,
				Reason:    fmt.Sprintf("matched tag [%s]", tag),
			}}, true
		}
	}
	return nil, false
}

type durationMatcher struct {
	logger   *zap.Logger
	duration time.Duration
}

func (m *durationMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	dur := res.Duration()
	if dur == 0 {
		dur = time.Millisecond * 1
	}

	if dur.Abs() >= m.duration {
		return []TriggerCondition{{
			Condition: ConditionDuration,
			Reason:    fmt.Sprintf("duration [%s] exceeded threshold [%s]", dur.Truncate(time.Millisecond).String(), m.duration.String()),
		}}, true
	}
	return nil, false
}

type bodyMatcher struct {
	logger *zap.Logger
	match  func(body []byte) ([]TriggerCondition, bool)
}

func (m *bodyMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	return m.match(res.Body())
}

// noopMatcher returns a fixed match result
type noopMatcher struct {
	match bool
}

func (m *noopMatcher) Match(req, res HTTPMessage) ([]TriggerCondition, bool) {
	return nil, m.match
}
