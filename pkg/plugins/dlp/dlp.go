package dlp

import (
	"regexp"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
)

type DLPEngine struct {
	Rules             []Rule
	ForceFailDetected bool
	DetectionCounts   map[string]int
	MatchDetails      map[string][]string
}

func NewDLPEngine(rules []Rule) *DLPEngine {
	return &DLPEngine{
		Rules:           rules,
		DetectionCounts: make(map[string]int),
		MatchDetails:    make(map[string][]string),
	}
}

// ProcessHeaders processes request headers and detects/redacts sensitive data based on rules
func (d *DLPEngine) ProcessHeaders(headers tools.Headers) bool {
	detectionMade := false

	for key := range headers.All() {
		headers.Values(key, func(value plugins.HeaderValue) {
			_, detected := d.ApplyRulesToString(value.String())
			// Eventually we will set the value, but we cannot right now.
			detectionMade = detectionMade || detected
		})
	}

	return detectionMade
}

// ApplyRulesToString applies DLP rules to a string and optionally redacts it
func (d *DLPEngine) ApplyRulesToString(input string) (string, bool) {
	detectionMade := false

	for _, rule := range d.Rules {
		newInput, matches := applyRuleToString(input, rule)
		if len(matches) > 0 {
			detectionMade = true
			input = newInput
			if rule.Report {
				d.DetectionCounts[rule.Label] += len(matches)
			}
			if rule.Record {
				d.MatchDetails[rule.Label] = append(d.MatchDetails[rule.Label], matches...)
			}
			if rule.FailOnDetect {
				d.ForceFailDetected = true
			}
		}
	}

	return input, detectionMade
}

// applyRuleToString checks a string against a rule and applies redaction if needed
func applyRuleToString(input string, rule Rule) (string, []string) {
	regex := regexp.MustCompile(rule.Expression)
	matches := regex.FindAllString(input, -1)

	if len(matches) > 0 && rule.Scrub {
		input = regex.ReplaceAllString(input, "["+rule.Label+"]")
	}

	return input, matches
}

func (d *DLPEngine) GetRule(name string) *Rule {
	for _, r := range d.Rules {
		if r.Label == name {
			return &r
		}
	}

	return nil
}
