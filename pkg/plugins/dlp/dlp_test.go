package dlp

import (
	"reflect"
	"testing"
)

// Sample rules for demonstration purposes.
// In practice, you should define these according to actual regex patterns and labels.
var (
	RulePhoneNumber          = Rule{Label: "PHONE NUMBER", Expression: `123-456-7890`, Scrub: true}
	RuleCreditCardNumber     = Rule{Label: "CREDIT CARD NUMBER", Expression: `1234-5678-9012-3456`, Scrub: true}
	RuleSinNumber            = Rule{Label: "SIN NUMBER", Expression: `123-456-789`, Scrub: true}
	RuleSocialSecurityNumber = Rule{Label: "SOCIAL SECURITY NUMBER", Expression: `123-45-6789`, Scrub: true}
	RuleEmailAddress         = Rule{Label: "EMAIL ADDRESS", Expression: `email@example.com`, Scrub: true}
	RulePostalCode           = Rule{Label: "POSTAL CODE", Expression: `M4B 1C3`, Scrub: true}
	RuleZipCode              = Rule{Label: "ZIP CODE", Expression: `12345(-6789)?`, Scrub: true}
)

func TestApplyRuleToString(t *testing.T) {
	testCases := []struct {
		description     string
		input           string
		rule            Rule
		expectedOutput  string
		expectedMatches []string
	}{
		{
			description:     "Scrubbing phone number",
			input:           "Call me at 123-456-7890.",
			rule:            RulePhoneNumber,
			expectedOutput:  "Call me at [PHONE NUMBER].",
			expectedMatches: []string{"123-456-7890"},
		},
		{
			description:     "No scrubbing phone number",
			input:           "Call me at 123-456-7890.",
			rule:            Rule{Label: "PHONE NUMBER", Expression: `123-456-7890`, Scrub: false},
			expectedOutput:  "Call me at 123-456-7890.",
			expectedMatches: []string{"123-456-7890"},
		},
		{
			description:     "Scrubbing credit card number",
			input:           "My card number is 1234-5678-9012-3456.",
			rule:            RuleCreditCardNumber,
			expectedOutput:  "My card number is [CREDIT CARD NUMBER].",
			expectedMatches: []string{"1234-5678-9012-3456"},
		},
		{
			description:     "No scrubbing credit card number",
			input:           "My card number is 1234-5678-9012-3456.",
			rule:            Rule{Label: "CREDIT CARD NUMBER", Expression: `1234-5678-9012-3456`, Scrub: false},
			expectedOutput:  "My card number is 1234-5678-9012-3456.",
			expectedMatches: []string{"1234-5678-9012-3456"},
		},
		{
			description:     "Scrubbing SIN number",
			input:           "My SIN is 123-456-789.",
			rule:            RuleSinNumber,
			expectedOutput:  "My SIN is [SIN NUMBER].",
			expectedMatches: []string{"123-456-789"},
		},
		{
			description:     "No scrubbing SIN number",
			input:           "My SIN is 123-456-789.",
			rule:            Rule{Label: "SIN NUMBER", Expression: `123-456-789`, Scrub: false},
			expectedOutput:  "My SIN is 123-456-789.",
			expectedMatches: []string{"123-456-789"},
		},
		{
			description:     "Scrubbing Social Security number",
			input:           "My SSN is 123-45-6789.",
			rule:            RuleSocialSecurityNumber,
			expectedOutput:  "My SSN is [SOCIAL SECURITY NUMBER].",
			expectedMatches: []string{"123-45-6789"},
		},
		{
			description:     "No scrubbing Social Security number",
			input:           "My SSN is 123-45-6789.",
			rule:            Rule{Label: "SOCIAL SECURITY NUMBER", Expression: `123-45-6789`, Scrub: false},
			expectedOutput:  "My SSN is 123-45-6789.",
			expectedMatches: []string{"123-45-6789"},
		},
		{
			description:     "Scrubbing email address",
			input:           "Contact me at email@example.com.",
			rule:            RuleEmailAddress,
			expectedOutput:  "Contact me at [EMAIL ADDRESS].",
			expectedMatches: []string{"email@example.com"},
		},
		{
			description:     "No scrubbing email address",
			input:           "Contact me at email@example.com.",
			rule:            Rule{Label: "EMAIL ADDRESS", Expression: `email@example.com`, Scrub: false},
			expectedOutput:  "Contact me at email@example.com.",
			expectedMatches: []string{"email@example.com"},
		},
		{
			description:     "Scrubbing postal code",
			input:           "My postal code is M4B 1C3.",
			rule:            RulePostalCode,
			expectedOutput:  "My postal code is [POSTAL CODE].",
			expectedMatches: []string{"M4B 1C3"},
		},
		{
			description:     "No scrubbing postal code",
			input:           "My postal code is M4B 1C3.",
			rule:            Rule{Label: "POSTAL CODE", Expression: `M4B 1C3`, Scrub: false},
			expectedOutput:  "My postal code is M4B 1C3.",
			expectedMatches: []string{"M4B 1C3"},
		},
		{
			description:     "Scrubbing ZIP code",
			input:           "My ZIP code is 12345-6789.",
			rule:            RuleZipCode,
			expectedOutput:  "My ZIP code is [ZIP CODE].",
			expectedMatches: []string{"12345-6789"},
		},
		{
			description:     "No scrubbing ZIP code",
			input:           "My ZIP code is 12345-6789.",
			rule:            Rule{Label: "ZIP CODE", Expression: `12345(-6789)?`, Scrub: false},
			expectedOutput:  "My ZIP code is 12345-6789.",
			expectedMatches: []string{"12345-6789"},
		},
		{
			description:     "Scrubbing ZIP code without extension",
			input:           "My ZIP code is 12345.",
			rule:            RuleZipCode,
			expectedOutput:  "My ZIP code is [ZIP CODE].",
			expectedMatches: []string{"12345"},
		},
		{
			description:     "No scrubbing ZIP code without extension",
			input:           "My ZIP code is 12345.",
			rule:            Rule{Label: "ZIP CODE", Expression: `12345`, Scrub: false},
			expectedOutput:  "My ZIP code is 12345.",
			expectedMatches: []string{"12345"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			output, matches := applyRuleToString(tc.input, tc.rule)
			if output != tc.expectedOutput {
				t.Errorf("Expected output %v, got %v", tc.expectedOutput, output)
			}
			if !reflect.DeepEqual(matches, tc.expectedMatches) {
				t.Errorf("Expected matches %v, got %v", tc.expectedMatches, matches)
			}
		})
	}
}
