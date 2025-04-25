package config

import (
	"fmt"
	"slices"
	"strings"

	validator "github.com/go-playground/validator/v10"
	"github.com/qpoint-io/rulekit"
	yaml "gopkg.in/yaml.v3"
)

type Plugin struct {
	Type   string    `yaml:"type" validate:"required"`
	Config yaml.Node `yaml:"config"`
}

type Stack struct {
	Plugins []Plugin `yaml:"plugins" validate:"dive"`
}

type Services struct {
	EventStores  []ServiceEventStore  `yaml:"event_stores"`
	ObjectStores []ServiceObjectStore `yaml:"object_stores"`
	QscanClient  *ServiceQscan        `yaml:"qscan"`
}

func (s Services) ToMap() map[string]any {
	m := make(map[string]any)

	m[s.FirstEventStore().ServiceType()] = s.FirstEventStore()

	m[s.FirstObjectStore().ServiceType()] = s.FirstObjectStore()

	m[s.Qscan().ServiceType()] = s.Qscan()

	return m
}

func (s Services) Qscan() ServiceQscan {
	if s.QscanClient == nil {
		return ServiceQscan{
			Type: QscanType_DISABLED,
		}
	}

	return *s.QscanClient
}

func (s Services) HasAnyEventStores() bool {
	return len(s.EventStores) > 0
}

func (s Services) FirstEventStore() ServiceEventStore {
	if len(s.EventStores) == 0 {
		return ServiceEventStore{
			Type: EventStoreType_DISABLED,
		}
	}

	return s.EventStores[0]
}

func (s Services) HasAnyObjectStores() bool {
	return len(s.ObjectStores) > 0
}

func (s Services) FirstObjectStore() ServiceObjectStore {
	if len(s.ObjectStores) == 0 {
		return ServiceObjectStore{
			Type: ObjectStoreType_DISABLED,
		}
	}

	return s.ObjectStores[0]
}

type Config struct {
	Stacks   map[string]Stack `yaml:"stacks" validate:"dive"`
	Tap      *TapConfig       `yaml:"tap"`
	Services Services         `yaml:"services"`
	Control  *Control         `yaml:"control"`
}

type Control struct {
	Default AccessControlAction `yaml:"default" validate:"required,access_control_default_action"`
	Rules   []Rule              `yaml:"rules" validate:"dive"`
}

type Rule struct {
	Name    string                `yaml:"name" validate:"required"`
	Expr    string                `yaml:"expr" validate:"required,rule_expression"`
	Actions []AccessControlAction `yaml:"actions" validate:"omitempty,dive,required,access_control_action"`
}

func (c *Config) SetDefaults() {
	if c.Control != nil && c.Control.Default == "" {
		c.Control.Default = AccessControlAction_ALLOW
	}
}

func (c *Config) Normalize() {
	if c.Control != nil {
		for _, rule := range c.Control.Rules {
			for i, action := range rule.Actions {
				rule.Actions[i] = AccessControlAction(strings.ToLower(string(action)))
			}
		}
	}
}

func (c *Config) Validate() error {
	validate := validator.New()

	for name, fn := range map[string]validator.Func{
		"stringnotempty":                validateStringNotEmpty,
		"access_control_action":         ValidateAccessControlAction,
		"access_control_default_action": ValidateAccessControlDefaultAction,
		"rule_expression":               ValidateRuleExpression,
	} {
		if err := validate.RegisterValidation(name, fn); err != nil {
			return fmt.Errorf("failed to register %s validation: %w", name, err)
		}
	}

	c.SetDefaults()
	c.Normalize()

	return validate.Struct(c)
}

func validateStringNotEmpty(fl validator.FieldLevel) bool {
	return len(fl.Field().String()) != 0
}

// ValidateAccessControlAction validates that the field is a valid access control action
func ValidateAccessControlAction(fl validator.FieldLevel) bool {
	return slices.Contains([]AccessControlAction{
		AccessControlAction_ALLOW,
		AccessControlAction_DENY,
		// AccessControlAction_LOG, // TODO(ENG-321)
	}, AccessControlAction(fl.Field().String()))
}

// ValidateAccessControlDefaultAction validates that the field is a valid default access control action
func ValidateAccessControlDefaultAction(fl validator.FieldLevel) bool {
	return slices.Contains([]AccessControlAction{
		AccessControlAction_ALLOW,
		// AccessControlAction_DENY, // TODO(ENG-320)
	}, AccessControlAction(fl.Field().String()))
}

// ValidateRuleExpression validates that the field is a valid rule expression
func ValidateRuleExpression(fl validator.FieldLevel) bool {
	_, err := rulekit.Parse(fl.Field().String())
	return err == nil
}

func UnmarshalConfig(bytes []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(bytes, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
