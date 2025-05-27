package rulekitsvc

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/rulekit"
)

const (
	TypeRulekitMacros services.ServiceType = "rulekit.macros"
)

type Macros interface {
	services.Service
	Macros() map[string]rulekit.Rule
}

type MacrosFactory struct {
	Macros map[string]rulekit.Rule
}

func (f *MacrosFactory) FactoryType() services.ServiceType {
	return TypeRulekitMacros
}

func (f *MacrosFactory) ServiceType() services.ServiceType {
	return TypeRulekitMacros
}

func (f *MacrosFactory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(*config.Rulekit)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted *config.Rulekit", cfg)
	}

	macros, err := c.ParseMacros()
	if err != nil {
		return fmt.Errorf("parsing rulekit macros: %w", err)
	}
	if err := config.ValidateRulekitMacros(macros); err != nil {
		return fmt.Errorf("validating rulekit macros: %w", err)
	}

	f.Macros = macros
	return nil
}

func (f *MacrosFactory) Create(ctx context.Context) (services.Service, error) {
	return macrosService(f.Macros), nil
}

type macrosService map[string]rulekit.Rule

func (s macrosService) ServiceType() services.ServiceType {
	return TypeRulekitMacros
}

func (s macrosService) Macros() map[string]rulekit.Rule {
	return s
}
