package rulekitsvc

import (
	"context"
	"fmt"
	"maps"
	"net"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/rulekitext"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/rulekit"
)

const (
	TypeRulekit services.ServiceType = "rulekit"
)

type Macros interface {
	services.Service
	Macros() map[string]rulekit.Rule
	Functions() map[string]*rulekit.Function
}

type Factory struct {
	Macros map[string]rulekit.Rule

	resolver *net.Resolver
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

	macros, err := c.ParseMacros()
	if err != nil {
		return fmt.Errorf("parsing rulekit macros: %w", err)
	}
	if err := config.ValidateRulekitMacros(macros); err != nil {
		return fmt.Errorf("validating rulekit macros: %w", err)
	}

	f.Macros = macros

	resolver := net.DefaultResolver
	if c.Resolver != "" {
		// custom dns resolver
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 10 * time.Second,
				}
				return d.DialContext(ctx, network, c.Resolver)
			},
		}
	}
	f.resolver = resolver
	return nil
}

func (f *Factory) Create(ctx context.Context) (services.Service, error) {
	return &macrosService{
		ctx:      ctx,
		macros:   f.Macros,
		resolver: f.resolver,
	}, nil
}

type macrosService struct {
	ctx      context.Context
	macros   map[string]rulekit.Rule
	resolver *net.Resolver
}

func (s *macrosService) ServiceType() services.ServiceType {
	return TypeRulekit
}

func (s *macrosService) Macros() map[string]rulekit.Rule {
	return s.macros
}

func (s *macrosService) Functions() map[string]*rulekit.Function {
	funcs := make(map[string]*rulekit.Function)
	maps.Copy(funcs, rulekitext.Functions)

	funcs["nslookup"] = &rulekit.Function{
		Args: []rulekit.FunctionArg{
			{Name: "host"},
		},
		Eval: s.nslookup,
	}
	return funcs
}

func (s *macrosService) nslookup(args rulekit.KV) rulekit.Result {
	host, err := rulekit.IndexFuncArg[string](args, "host")
	if err != nil {
		return rulekit.Result{Error: err}
	}

	ips, err := s.resolver.LookupIP(s.ctx, "ip", host)
	if err != nil {
		return rulekit.Result{Error: err}
	}
	arr := make([]any, len(ips))
	for i, ip := range ips {
		arr[i] = ip
	}
	return rulekit.Result{Value: arr}
}
