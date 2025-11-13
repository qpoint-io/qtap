package config

import (
	"fmt"
	"slices"
	"strings"

	validator "github.com/go-playground/validator/v10"
	"github.com/qpoint-io/qtap/pkg/rulekitext"
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

// All returns all service configs.
// For event stores and object stores, the first ones are set as the default.
func (s Services) All() []*ServiceConfig {
	qscans := s.GetQscans()
	eventStores := s.GetEventStores()
	objectStores := s.GetObjectStores()

	configs := make([]*ServiceConfig, 0, len(eventStores)+len(objectStores)+len(qscans))
	configs = append(configs, eventStores...)
	configs = append(configs, objectStores...)
	configs = append(configs, qscans...)
	return configs
}

type ServiceConfig struct {
	// ID is optional.
	// If not set, the service will be registered as the default for its service type.
	ID string
	// Type is th factory type.
	// For example, "eventstore.console".
	Type string
	// Config is passed to the factory's Init method.
	Config any

	// Default indicates that this service should be used as the default for its service type
	// even if an ID is provided.
	Default bool
}

func (s Services) GetQscans() []*ServiceConfig {
	qc := s.QscanClient
	if qc == nil {
		qc = &ServiceQscan{Type: QscanType_DISABLED}
	}

	return []*ServiceConfig{{
		Type:    qc.ServiceType(),
		Config:  qc,
		Default: true,
	}}
}

func (s Services) HasAnyEventStores() bool {
	return len(s.EventStores) > 0
}

func (s Services) GetEventStores() []*ServiceConfig {
	stores := s.EventStores
	if len(stores) == 0 {
		stores = []ServiceEventStore{{Type: EventStoreType_DISABLED}}
	}

	configs := make([]*ServiceConfig, len(stores))
	for i, store := range stores {
		isDefault := i == 0
		id := store.ID
		if id == "" && !isDefault {
			// the service registry will treat event stores without an ID as the default.
			// however, we want to explicitly set the first event store as the default regardless of IDs.
			// => if this is not the first event store, generate a unique ID for it
			id = fmt.Sprintf("eventstore-%d", i)
		}

		configs[i] = &ServiceConfig{
			Type:    store.ServiceType(),
			Config:  store,
			Default: isDefault,
			ID:      id,
		}
	}
	return configs
}

func (s Services) HasAnyObjectStores() bool {
	return len(s.ObjectStores) > 0
}

func (s Services) GetObjectStores() []*ServiceConfig {
	stores := s.ObjectStores
	if len(stores) == 0 {
		stores = []ServiceObjectStore{{Type: ObjectStoreType_DISABLED}}
	}

	configs := make([]*ServiceConfig, len(stores))
	for i, store := range stores {
		isDefault := i == 0
		id := store.ID
		if id == "" && !isDefault {
			// the service registry will treat object stores without an ID as the default.
			// however, we want to explicitly set the first object store as the default regardless of IDs.
			// => if this is not the first object store, generate a unique ID for it
			id = fmt.Sprintf("objectstore-%d", i)
		}

		configs[i] = &ServiceConfig{
			Type:    store.ServiceType(),
			Config:  store,
			Default: isDefault,
			ID:      id,
		}
	}
	return configs
}

type Config struct {
	Stacks   map[string]Stack `yaml:"stacks" validate:"dive"`
	Tap      *TapConfig       `yaml:"tap"`
	Services Services         `yaml:"services"`
	Tags     []Tag            `yaml:"tags" validate:"omitempty,dive"`
	Control  *Control         `yaml:"control"`
	Rulekit  *Rulekit         `yaml:"rulekit"`
}

type Tag struct {
	Key      string `yaml:"key" validate:"required"`
	Source   string `yaml:"source" validate:"required,oneof=env k8s.label k8s.annotation container.label"`
	Location string `yaml:"location" validate:"required"`
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

type Rulekit struct {
	Macros []*RulekitMacro `yaml:"macros" validate:"omitempty,dive"`
}

type RulekitMacro struct {
	Name string `yaml:"name" validate:"required"`
	Expr string `yaml:"expr" validate:"required,rule_expression"`
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

	if c.Rulekit != nil {
		macros, err := c.Rulekit.ParseMacros()
		if err != nil {
			return fmt.Errorf("parsing rulekit macros: %w", err)
		}

		if err := ValidateRulekitMacros(macros); err != nil {
			return fmt.Errorf("validating rulekit macros: %w", err)
		}
	}

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

func (r *Rulekit) ParseMacros() (map[string]rulekit.Rule, error) {
	macros := make(map[string]rulekit.Rule, len(r.Macros))
	for _, macro := range r.Macros {
		if _, exists := macros[macro.Name]; exists {
			return nil, fmt.Errorf("%q: name must be unique", macro.Name)
		}

		r, err := rulekit.Parse(macro.Expr)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", macro.Name, err)
		}

		macros[macro.Name] = r
	}

	return macros, nil
}

func ValidateRulekitMacros(macros map[string]rulekit.Rule) error {
	err := (&rulekit.Ctx{
		Functions: rulekitext.Functions,
		Macros:    macros,
	}).Validate()
	if err != nil {
		return err
	}

	return nil
}

func UnmarshalConfig(bytes []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(bytes, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
