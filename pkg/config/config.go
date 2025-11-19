package config

import (
	"fmt"
	"strconv"

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
}

// All returns all service configs.
// For event stores and object stores, the first ones are set as the default.
func (s Services) All() []*ServiceConfig {
	eventStores := s.GetEventStores()
	objectStores := s.GetObjectStores()

	configs := make([]*ServiceConfig, 0, len(eventStores)+len(objectStores))
	configs = append(configs, eventStores...)
	configs = append(configs, objectStores...)
	return configs
}

type ServiceConfig struct {
	// ID is optionally used to identify this specific service instance by other services or plugins.
	ID string
	// Type is the factory type.
	// For example, "eventstore.console".
	Type string
	// Config is passed to the factory's Init method.
	Config any
	// Default indicates that this service should be used as the default for its service type.
	Default bool
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
		isDefault := i == 0 // the first event store in the config is the default

		configs[i] = &ServiceConfig{
			Type:    store.ServiceType(),
			Config:  store,
			Default: isDefault,
			ID:      store.ID,
		}
	}
	return configs
}

func (s Services) GetEventStoresByID() map[string]*ServiceEventStore {
	stores := make(map[string]*ServiceEventStore, len(s.EventStores))
	for _, store := range s.EventStores {
		stores[store.ID] = &store
	}
	return stores
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
		isDefault := i == 0 // the first object store in the config is the default

		configs[i] = &ServiceConfig{
			Type:    store.ServiceType(),
			Config:  store,
			Default: isDefault,
			ID:      store.ID,
		}
	}
	return configs
}

type Config struct {
	Stacks   map[string]Stack `yaml:"stacks" validate:"dive"`
	Tap      *TapConfig       `yaml:"tap"`
	Services Services         `yaml:"services"`
	Tags     []Tag            `yaml:"tags" validate:"omitempty,dive"`
	Rulekit  *Rulekit         `yaml:"rulekit"`
}

type Tag struct {
	Key      string `yaml:"key" validate:"required"`
	Source   string `yaml:"source" validate:"required,oneof=env k8s.label k8s.annotation container.label"`
	Location string `yaml:"location" validate:"required"`
}

type Rulekit struct {
	Macros []*RulekitMacro `yaml:"macros" validate:"omitempty,dive"`
}

type RulekitMacro struct {
	Name string `yaml:"name" validate:"required"`
	Expr string `yaml:"expr" validate:"required,rule_expression"`
}

func (c *Config) Validate() error {
	validate := validator.New()

	for name, fn := range map[string]validator.Func{
		"stringnotempty":  validateStringNotEmpty,
		"rule_expression": ValidateRuleExpression,
	} {
		if err := validate.RegisterValidation(name, fn); err != nil {
			return fmt.Errorf("failed to register %s validation: %w", name, err)
		}
	}

	if c.Rulekit != nil {
		macros, err := c.Rulekit.ParseMacros()
		if err != nil {
			return fmt.Errorf("parsing rulekit macros: %w", err)
		}

		if err := ValidateRulekitMacros(macros); err != nil {
			return fmt.Errorf("validating rulekit macros: %w", err)
		}
	}

	if len(c.Services.ObjectStores) > 0 {
		eventStores := c.Services.GetEventStoresByID()
		for i, objectStore := range c.Services.ObjectStores {
			if objectStore.EventStore.Disabled {
				continue
			}

			if objectStore.EventStore.ID != "" {
				if _, exists := eventStores[objectStore.EventStore.ID]; !exists {
					osID := objectStore.ID
					if osID == "" {
						osID = strconv.Itoa(i)
					}
					return fmt.Errorf("object store %s: event store id=%q does not exist", osID, objectStore.EventStore.ID)
				}
			}
		}
	}

	return validate.Struct(c)
}

func validateStringNotEmpty(fl validator.FieldLevel) bool {
	return len(fl.Field().String()) != 0
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
