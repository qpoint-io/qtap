package rulekitsvc

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/rulekitext"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/rulekit"
)

const (
	TypeRulekit services.ServiceType = "rulekit"
)

type Service interface {
	services.Service
	Macros() map[string]rulekit.Rule
	Functions() map[string]*rulekit.Function
	Values() rulekit.KV
}

type Factory struct {
	Macros map[string]rulekit.Rule
}

func (f *Factory) FactoryType() services.ServiceType {
	return TypeRulekit
}

func (f *Factory) ServiceType() services.ServiceType {
	return TypeRulekit
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(*config.Rulekit)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted *config.Rulekit", cfg)
	}

	if c != nil {
		macros, err := c.ParseMacros()
		if err != nil {
			return fmt.Errorf("parsing rulekit macros: %w", err)
		}
		if err := config.ValidateRulekitMacros(macros); err != nil {
			return fmt.Errorf("validating rulekit macros: %w", err)
		}

		f.Macros = macros
	}

	return nil
}

func (f *Factory) Create(ctx context.Context) (services.Service, error) {
	return &service{f: f}, nil
}

type service struct {
	f    *Factory
	conn *connection.Connection
}

func (s *service) ServiceType() services.ServiceType {
	return TypeRulekit
}

// SetConnection implements the ConnectionAdapter interface.
func (s *service) SetConnection(conn *connection.Connection) {
	s.conn = conn
}

func (s *service) Macros() map[string]rulekit.Rule {
	return s.f.Macros
}

func (s *service) Functions() map[string]*rulekit.Function {
	return rulekitext.Functions
}

func (s *service) Values() rulekit.KV {
	if s.conn == nil {
		return nil
	}

	return s.conn.ControlValues()
}
