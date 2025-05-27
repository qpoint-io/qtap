package config

import (
	"errors"
	"testing"

	validator "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidConfig tests the unmarshaling and validation of a valid YAML configuration
func TestValidConfig(t *testing.T) {
	yamlBlob := `
services:
  event_store:
    type: stdout
  object_store:
    type: stdout
tap:
  audit_logs:
    type: disabled
  default_stack: complete
proxy:
  tcp_listen_list: "0.0.0.0:10080:80,0.0.0.0:10443:443"
  dns_lookup_family: "ALL"
  default_domain_action: "ALLOW"
  default_ip_address_action: "ALLOW"
  endpoints:
  - domain: "example.com"
    action: "ALLOW"
`

	config, err := UnmarshalConfig([]byte(yamlBlob))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	err = config.Validate()
	if err != nil {
		t.Errorf("Validation failed: %v", err)
	}
}

// TestInvalidConfig tests the unmarshaling and validation of an invalid YAML configuration
// func TestInvalidConfig(t *testing.T) {
// 	yamlBlob := `
// proxy:
//   tcp_listen_list: "0.0.0.0:10080:80,0.0.0.0:10443:443"
//   dns_lookup_family: "ALL"
//   default_domain_action: "ALLOW"
//   default_ip_address_action: "ALLOW"
//   endpoints:
//     - domain: "example.com"
//       action: "DENY"
// `

// 	config, err := UnmarshalConfig([]byte(yamlBlob))
// 	if err != nil {
// 		t.Fatalf("Failed to unmarshal: %v", err)
// 	}

// 	err = config.Validate()
// 	if err == nil {
// 		t.Errorf("Expected validation to fail, but it passed")
// 	}
// }

// TestValidConfig tests the unmarshaling and validation of a valid YAML configuration
func TestValidConfigStacks(t *testing.T) {
	yamlBlob := `
services:
  event_store:
    type: stdout
  object_store:
    type: stdout
stacks:
  default:
    middlewares:
      - name: report
        config: "{<JSON>}"
        wasm: "https://some-location-to.wasm"
  deep-scan:
    middlewares:
      - name: report
        config: "{<JSON>}"
        wasm: "https://some-location-to.wasm"
      - name: scan
        config: "{<JSON>}"
        wasm: "https://some-location-to.wasm"
tap:
  audit_logs:
    type: disabled
  default_stack: complete
proxy:
  tcp_listen_list: "0.0.0.0:10080:80,0.0.0.0:10443:443"
  dns_lookup_family: "ALL"
  default_domain_action: "ALLOW"
  default_ip_address_action: "ALLOW"
  endpoints:
    - domain: "example.com"
      action: "ALLOW"
`

	config, err := UnmarshalConfig([]byte(yamlBlob))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	err = config.Validate()
	if err != nil {
		t.Errorf("Validation failed: %v", err)
	}
}

func TestValidate(t *testing.T) {
	tcs := []struct {
		name   string
		config *Config
		errors []string
		test   func(*testing.T, *Config, validator.ValidationErrors) // use validatorErrors if needed
	}{
		{
			name: "happy path",
			config: &Config{
				Tap: &TapConfig{},
				Control: &Control{
					Rules: []Rule{
						{
							Name:    "test",
							Expr:    "src.ip == 192.168.1.1",
							Actions: []AccessControlAction{AccessControlAction("aLLow")},
						},
					},
				},
			},
			test: func(t *testing.T, c *Config, errs validator.ValidationErrors) {
				assert.Equal(t, AccessControlAction_ALLOW, c.Control.Default)
				assert.Equal(t, []AccessControlAction{AccessControlAction_ALLOW}, c.Control.Rules[0].Actions) // normalized casing
			},
		},
		{
			name: "control invalid default",
			config: &Config{
				Control: &Control{
					Default: "log",
				},
			},
			errors: []string{
				"Key: 'Config.Control.Default' Error:Field validation for 'Default' failed on the 'access_control_default_action' tag",
			},
		},
		{
			name: "control invalid rule action",
			config: &Config{
				Control: &Control{
					Rules: []Rule{{Actions: []AccessControlAction{"invalid"}}},
				},
			},
			errors: []string{
				"Key: 'Config.Control.Rules[0].Name' Error:Field validation for 'Name' failed on the 'required' tag",
				"Key: 'Config.Control.Rules[0].Expr' Error:Field validation for 'Expr' failed on the 'required' tag",
				"Key: 'Config.Control.Rules[0].Actions[0]' Error:Field validation for 'Actions[0]' failed on the 'access_control_action' tag",
			},
		},
		{
			name: "control invalid rule expression",
			config: &Config{
				Control: &Control{
					Rules: []Rule{{Expr: "true !+ false"}},
				},
			},
			errors: []string{
				"Key: 'Config.Control.Rules[0].Name' Error:Field validation for 'Name' failed on the 'required' tag",
				"Key: 'Config.Control.Rules[0].Expr' Error:Field validation for 'Expr' failed on the 'rule_expression' tag",
			},
		},
		{
			name: "rulekit valid macros",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{{
						Name: "test",
						Expr: "src.ip == 192.168.1.1",
					}},
				},
			},
		},
		{
			name: "rulekit macro invalid syntax",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{{
						Name: "test",
						Expr: "+!&3",
					}},
				},
			},
			errors: []string{
				`parsing rulekit macros: "test": syntax error at line 1:1:
+!&3
^
syntax error: unexpected symbol`,
			},
		},
		{
			name: "rulekit macro name conflict with function",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{{
						Name: "zone",
						Expr: "src.ip == 192.168.1.1",
					}},
				},
			},
			errors: []string{
				`validating rulekit macros: macro "zone": name conflicts with a custom function`,
			},
		},
		{
			name: "rulekit duplicate macros",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{
						{
							Name: "zone",
							Expr: "src.ip == 192.168.1.1",
						},
						{
							Name: "zone",
							Expr: "src.ip == duplicate",
						},
					},
				},
			},
			errors: []string{
				`parsing rulekit macros: "zone": name must be unique`,
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if len(tc.errors) == 0 {
				require.NoError(t, err)
				if tc.test != nil {
					tc.test(t, tc.config, nil)
				}
				return
			}

			var verrs validator.ValidationErrors
			if errors.As(err, &verrs) {
				require.ElementsMatch(t, tc.errors, validatorErrors(verrs))
				if tc.test != nil {
					tc.test(t, tc.config, verrs)
				}
			} else {
				require.Len(t, tc.errors, 1, "got non-validator.ValidationErrors, test case errors must have exactly one element")
				require.EqualError(t, err, tc.errors[0])
			}
		})
	}
}

func validatorErrors(errs validator.ValidationErrors) []string {
	errors := []string{}
	for _, err := range errs {
		errors = append(errors, err.Error())
	}
	return errors
}
