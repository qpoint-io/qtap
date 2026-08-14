package errordetection

// import (
// 	"net/http"
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// 	"go.uber.org/zap"
// )

// // Mock implementation of HTTPMessage for testing
// type mockHTTPMessage struct {
// 	headers  http.Header
// 	body     []byte
// 	tags     []string
// 	duration time.Duration
// }

// func (m *mockHTTPMessage) Headers() *txs.HeaderMap {
// 	return txs.NewHeaderMap(NewHeaders(m.headers))
// }
// func (m *mockHTTPMessage) Body() []byte            { return m.body }
// func (m *mockHTTPMessage) Tags() []string          { return m.tags }
// func (m *mockHTTPMessage) Duration() time.Duration { return m.duration }

// func TestRuleSet(t *testing.T) {
// 	logger := zap.NewNop()

// 	tests := []struct {
// 		name         string
// 		matchers     MatcherGroup
// 		req          HTTPMessage
// 		res          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name: "all matchers pass",
// 			matchers: MatcherGroup{
// 				&statusCodeMatcher{logger: logger, codes: []string{"5xx"}},
// 				&mimeCategoryMatcher{logger: logger, categories: []string{"app"}},
// 			},
// 			req: &mockHTTPMessage{},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{
// 					"Content-Type": []string{"text/html"},
// 					":status":      []string{"500"},
// 				},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{
// 				{
// 					Condition: ConditionStatus,
// 					Reason:    "response status code [500] matched [5xx]",
// 				},
// 				{
// 					Condition: ConditionMIME,
// 					Reason:    "response content-type [text/html] matched category [app]",
// 				},
// 			},
// 		},
// 		{
// 			name: "one matcher fails",
// 			matchers: MatcherGroup{
// 				&statusCodeMatcher{logger: logger, codes: []string{"5xx"}},
// 				// mime should fail
// 				&mimeCategoryMatcher{logger: logger, categories: []string{"image"}},
// 			},
// 			req: &mockHTTPMessage{},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{
// 					"Content-Type": []string{"text/html"},
// 					":status":      []string{"500"},
// 				},
// 			},
// 			wantMatch: false,
// 		},
// 		{
// 			name:      "empty ruleset",
// 			matchers:  MatcherGroup{},
// 			req:       &mockHTTPMessage{},
// 			res:       &mockHTTPMessage{},
// 			wantMatch: true,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			triggers, match := tt.matchers.Match(tt.req, tt.res)
// 			assert.Equal(t, tt.wantMatch, match)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }

// func TestStatusCodeMatcher(t *testing.T) {
// 	logger := zap.NewNop()

// 	tests := []struct {
// 		name         string
// 		codes        []string
// 		res          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name:  "exact match",
// 			codes: []string{"500"},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{":status": []string{"500"}},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionStatus,
// 				Reason:    "response status code [500] matched [500]",
// 			}},
// 		},
// 		{
// 			name:  "wildcard match",
// 			codes: []string{"5xx"},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{":status": []string{"503"}},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionStatus,
// 				Reason:    "response status code [503] matched [5xx]",
// 			}},
// 		},
// 		{
// 			name:  "no match",
// 			codes: []string{"5xx"},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{":status": []string{"404"}},
// 			},
// 			wantMatch:    false,
// 			wantTriggers: nil,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			m := &statusCodeMatcher{logger: logger, codes: tt.codes}
// 			triggers, match := m.Match(&mockHTTPMessage{}, tt.res)
// 			assert.Equal(t, tt.wantMatch, match)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }

// func TestDurationMatcher(t *testing.T) {
// 	logger := zap.NewNop()

// 	tests := []struct {
// 		name         string
// 		duration     time.Duration
// 		res          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name:     "duration exceeds threshold",
// 			duration: 100 * time.Millisecond,
// 			res: &mockHTTPMessage{
// 				duration: 200*time.Millisecond + 235*time.Nanosecond,
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionDuration,
// 				Reason:    "duration [200ms] exceeded threshold [100ms]",
// 			}},
// 		},
// 		{
// 			name:     "duration below threshold",
// 			duration: 100 * time.Millisecond,
// 			res: &mockHTTPMessage{
// 				duration: 50 * time.Millisecond,
// 			},
// 			wantMatch:    false,
// 			wantTriggers: nil,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			m := &durationMatcher{logger: logger, duration: tt.duration}
// 			triggers, match := m.Match(&mockHTTPMessage{}, tt.res)
// 			assert.Equal(t, tt.wantMatch, match)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }

// func TestMimeCategoryMatcher(t *testing.T) {
// 	logger := zap.NewNop()

// 	tests := []struct {
// 		name         string
// 		categories   []string
// 		res          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name:       "matching category",
// 			categories: []string{"app"},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionMIME,
// 				Reason:    "response content-type [text/html; charset=utf-8] matched category [app]",
// 			}},
// 		},
// 		{
// 			name:       "non-matching category",
// 			categories: []string{"image"},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{"Content-Type": []string{"text/html"}},
// 			},
// 			wantMatch:    false,
// 			wantTriggers: nil,
// 		},
// 		{
// 			name:       "missing content-type",
// 			categories: []string{"text"},
// 			res: &mockHTTPMessage{
// 				headers: http.Header{},
// 			},
// 			wantMatch:    false,
// 			wantTriggers: nil,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			m := &mimeCategoryMatcher{logger: logger, categories: tt.categories}
// 			triggers, match := m.Match(&mockHTTPMessage{}, tt.res)
// 			gotCT, _ := tt.res.Headers().MimeCategory()
// 			assert.Equal(t, tt.wantMatch, match, gotCT)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }

// func TestURLMatcher(t *testing.T) {
// 	logger := zap.NewNop()

// 	tests := []struct {
// 		name         string
// 		include      []string
// 		exclude      []string
// 		req          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name:    "URL in include list",
// 			include: []string{"https://example.com/api/users", "https://example.com/api/posts"},
// 			exclude: []string{},
// 			req: &mockHTTPMessage{
// 				headers: http.Header{
// 					":scheme":    []string{"https"},
// 					":authority": []string{"example.com"},
// 					":path":      []string{"/api/users"},
// 				},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionURL,
// 				Reason:    "url in include list",
// 			}},
// 		},
// 		{
// 			name:    "URL in exclude list",
// 			include: []string{"https://example.com/api/users"},
// 			exclude: []string{"https://example.com/api/users/private"},
// 			req: &mockHTTPMessage{
// 				headers: http.Header{
// 					":scheme":    []string{"https"},
// 					":authority": []string{"example.com"},
// 					":path":      []string{"/api/users/private"},
// 				},
// 			},
// 			wantMatch: false,
// 		},
// 		{
// 			name:    "URL not in include list",
// 			include: []string{"https://example.com/api/users"},
// 			exclude: []string{},
// 			req: &mockHTTPMessage{
// 				headers: http.Header{
// 					":scheme":    []string{"https"},
// 					":authority": []string{"example.com"},
// 					":path":      []string{"/api/posts"},
// 				},
// 			},
// 			wantMatch: false,
// 		},
// 		{
// 			name:    "empty include list accepts all non-excluded",
// 			include: []string{},
// 			exclude: []string{"/api/private"},
// 			req: &mockHTTPMessage{
// 				headers: http.Header{
// 					":scheme":    []string{"https"},
// 					":authority": []string{"example.com"},
// 					":path":      []string{"/api/public"},
// 				},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionURL,
// 				Reason:    "url not in exclude list",
// 			}},
// 		},
// 		{
// 			name:    "missing path header",
// 			include: []string{"/api/users"},
// 			exclude: []string{},
// 			req: &mockHTTPMessage{
// 				headers: http.Header{
// 					":scheme":    []string{"https"},
// 					":authority": []string{"example.com"},
// 				},
// 			},
// 			wantMatch: false,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			m := &urlMatcher{logger: logger, include: tt.include, exclude: tt.exclude}
// 			triggers, match := m.Match(tt.req, &mockHTTPMessage{})
// 			assert.Equal(t, tt.wantMatch, match)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }

// func TestTagMatcher(t *testing.T) {
// 	logger := zap.NewNop()

// 	tests := []struct {
// 		name         string
// 		tags         []string
// 		req          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name: "matching tag",
// 			tags: []string{"error", "warning"},
// 			req: &mockHTTPMessage{
// 				tags: []string{"info", "error", "debug"},
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionTag,
// 				Reason:    "matched tag [error]",
// 			}},
// 		},
// 		{
// 			name: "no matching tag",
// 			tags: []string{"error", "warning"},
// 			req: &mockHTTPMessage{
// 				tags: []string{"info", "debug"},
// 			},
// 			wantMatch:    false,
// 			wantTriggers: nil,
// 		},
// 		{
// 			name: "empty request tags",
// 			tags: []string{"error"},
// 			req: &mockHTTPMessage{
// 				tags: []string{},
// 			},
// 			wantMatch: false,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			m := &tagMatcher{logger: logger, tags: tt.tags}
// 			triggers, match := m.Match(tt.req, &mockHTTPMessage{})
// 			assert.Equal(t, tt.wantMatch, match)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }

// func TestBodyMatcher(t *testing.T) {
// 	logger := zap.NewNop()
// 	bodyLengthMatcher := func(body []byte) ([]TriggerCondition, bool) {
// 		if len(body) > 10 {
// 			return []TriggerCondition{{

// 				Condition: ConditionBody,
// 				Reason:    "response body is long enough",
// 			}}, true
// 		}
// 		return nil, false
// 	}

// 	tests := []struct {
// 		name         string
// 		matcher      func([]byte) ([]TriggerCondition, bool)
// 		res          HTTPMessage
// 		wantMatch    bool
// 		wantTriggers []TriggerCondition
// 	}{
// 		{
// 			name:    "matching body condition",
// 			matcher: bodyLengthMatcher,
// 			res: &mockHTTPMessage{
// 				body: []byte("this is a long enough body"),
// 			},
// 			wantMatch: true,
// 			wantTriggers: []TriggerCondition{{

// 				Condition: ConditionBody,
// 				Reason:    "response body is long enough",
// 			}},
// 		},
// 		{
// 			name:    "non-matching body condition",
// 			matcher: bodyLengthMatcher,
// 			res: &mockHTTPMessage{
// 				body: []byte("short"),
// 			},
// 			wantMatch:    false,
// 			wantTriggers: nil,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			m := &bodyMatcher{logger: logger, match: tt.matcher}
// 			triggers, match := m.Match(&mockHTTPMessage{}, tt.res)
// 			assert.Equal(t, tt.wantMatch, match)
// 			assert.Equal(t, tt.wantTriggers, triggers)
// 		})
// 	}
// }
