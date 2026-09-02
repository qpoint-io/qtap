package errordetection

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type DetectErrorConfig struct {
	Rules []Rule `json:"rules" yaml:"rules"`
}

type RulePersists struct {
	RecordReqHeaders bool `json:"recordReqHeaders,omitempty" yaml:"record_req_headers,omitempty"`
	RecordReqBody    bool `json:"recordReqBody,omitempty" yaml:"record_req_body,omitempty"`
	RecordResHeaders bool `json:"recordResHeaders,omitempty" yaml:"record_res_headers,omitempty"`
	RecordResBody    bool `json:"recordResBody,omitempty" yaml:"record_res_body,omitempty"`
}

func (r *RulePersists) Any() bool {
	return r.RecordReqHeaders || r.RecordReqBody || r.RecordResHeaders || r.RecordResBody
}

type Rule struct {
	Name               string   `json:"name" yaml:"name"`
	TriggerStatusCodes []string `json:"triggerStatusCodes,omitempty" yaml:"trigger_status_codes,omitempty"`
	TriggerEmptyBody   bool     `json:"triggerEmptyBody,omitempty" yaml:"trigger_empty_body,omitempty"`
	TriggerDuration    Duration `json:"triggerDuration" yaml:"trigger_duration,omitempty"`
	TriggerContains    string   `json:"triggerContains,omitempty" yaml:"trigger_contains,omitempty"`
	OnlyCategories     []string `json:"onlyCategories,omitempty" yaml:"only_categories,omitempty"`
	OnlyUrls           []string `json:"onlyUrls,omitempty" yaml:"only_urls,omitempty"`
	ExcludeUrls        []string `json:"excludeUrls,omitempty" yaml:"exclude_urls,omitempty"`
	WithTags           []string `json:"withTags,omitempty" yaml:"with_tags,omitempty"`
	ReportAsIssue      bool     `json:"reportAsIssue,omitempty" yaml:"report_as_issue,omitempty"`
	// Continue TODO: unimplemented
	Continue bool `json:"continue,omitempty" yaml:"continue,omitempty"`

	RulePersists `json:",inline" yaml:",inline"`
}

func (r *Rule) Matcher(logger *zap.Logger) Matcher {
	var rs MatcherGroup

	if len(r.OnlyCategories) > 0 {
		rs = append(rs, &mimeCategoryMatcher{logger, r.OnlyCategories})
	}
	if len(r.TriggerStatusCodes) > 0 {
		rs = append(rs, &statusCodeMatcher{logger, r.TriggerStatusCodes})
	}
	if len(r.OnlyUrls) > 0 || len(r.ExcludeUrls) > 0 {
		rs = append(rs, &urlMatcher{
			logger:  logger,
			include: r.OnlyUrls,
			exclude: r.ExcludeUrls,
		})
	}
	if len(r.WithTags) > 0 {
		rs = append(rs, &tagMatcher{logger, r.WithTags})
	}
	if r.TriggerDuration.Duration > 0 {
		rs = append(rs, &durationMatcher{logger, r.TriggerDuration.Duration})
	}
	if r.TriggerEmptyBody {
		rs = append(rs, &bodyMatcher{logger, func(body []byte) ([]TriggerCondition, bool) {
			if len(body) == 0 {
				return []TriggerCondition{{

					Condition: ConditionBody,
					Reason:    "response body is empty",
				}}, true
			}
			return nil, false
		}})
	}
	if r.TriggerContains != "" {
		rs = append(rs, &bodyMatcher{logger, func(body []byte) ([]TriggerCondition, bool) {
			if strings.Contains(string(body), r.TriggerContains) {
				return []TriggerCondition{{

					Condition: ConditionBody,
					Reason:    "response body contains the specified content",
				}}, true
			}
			return nil, false
		}})
	}

	if len(rs) == 0 {
		// if no rules are defined, match everything
		rs = append(rs, &noopMatcher{match: false})
	}

	return rs
}

type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value) * time.Millisecond
		return nil
	case string:
		var err error
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			// value is a string containing digits only. add ms suffix so it can be parsed as a duration
			value += "ms"
		}
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return errors.New("invalid duration")
	}
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var v any
	if err := value.Decode(&v); err != nil {
		return err
	}

	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value) * time.Millisecond
		return nil
	case int:
		d.Duration = time.Duration(value) * time.Millisecond
		return nil
	case string:
		var err error
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			// value is a string containing digits only. add ms suffix so it can be parsed as a duration
			value += "ms"
		}
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("invalid duration: %T", value)
	}
}

type TriggerCondition struct {
	Condition Condition `json:"condition" yaml:"condition"`
	Reason    string    `json:"reason" yaml:"reason"`
}

type Condition string

const (
	ConditionStatus   Condition = "status"
	ConditionMIME     Condition = "mime"
	ConditionURL      Condition = "url"
	ConditionBody     Condition = "body"
	ConditionTag      Condition = "tag"
	ConditionDuration Condition = "duration"
)

type TriggerConditions []TriggerCondition

func (t TriggerConditions) ToConditions() []eventstore.IssueTriggerCondition {
	if len(t) == 0 {
		return nil
	}

	conds := make([]eventstore.IssueTriggerCondition, len(t))
	for i, c := range t {
		conds[i] = eventstore.IssueTriggerCondition{
			Plugin:    eventstore.PluginNameDetectErrors,
			Condition: string(c.Condition),
		}
	}
	return conds
}

func (t TriggerConditions) ToReasons() []string {
	if len(t) == 0 {
		return nil
	}

	reasons := make([]string, len(t))
	for i, c := range t {
		reasons[i] = c.Reason
	}
	return reasons
}
