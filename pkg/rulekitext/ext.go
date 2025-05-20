package rulekitext

import (
	"strings"

	"github.com/qpoint-io/rulekit"
	"golang.org/x/net/publicsuffix"
)

// register all functions
var Functions = map[string]*rulekit.Function{
	// zone
	//
	// zone(dst.domain)
	//   -> {"dst.domain": "example.com"}     -> "example.com"
	//   -> {"dst.domain": "api.example.com"} -> "example.com"
	"zone": {
		Args: []rulekit.FunctionArg{
			{Name: "domain"},
		},
		Eval: func(args rulekit.KV) rulekit.Result {
			domain, err := rulekit.IndexFuncArg[string](args, "domain")
			if err != nil {
				return rulekit.Result{Error: err}
			}

			return rulekit.Result{Value: Zone(domain)}
		},
	},

	// in_zone
	//
	// in_zone(dst.domain, "example.com")
	//   -> {"dst.domain": "example.com"}     -> true
	//   -> {"dst.domain": "api.example.com"} -> true
	"in_zone": {
		Args: []rulekit.FunctionArg{
			{Name: "domain"},
			{Name: "zone"},
		},
		Eval: func(args rulekit.KV) rulekit.Result {
			domain, err := rulekit.IndexFuncArg[string](args, "domain")
			if err != nil {
				return rulekit.Result{Error: err}
			}

			zone, err := rulekit.IndexFuncArg[string](args, "zone")
			if err != nil {
				return rulekit.Result{Error: err}
			}

			return rulekit.Result{Value: InZone(domain, zone)}
		},
	},
}

// Zone returns the effective TLD+1 of a domain.
//
//	Zone("example.com")     -> "example.com"
//	Zone("sub.example.com") -> "example.com"
var Zone = func(domain string) string {
	domain = strings.TrimSuffix(domain, ".")
	if zone, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		return zone
	}

	return domain
}

// InZone returns true if the domain is the same as the zone or is a subdomain of the zone.
//
//	InZone(    "example.com", "example.com") -> true
//	InZone("sub.example.com", "example.com") -> true
//	InZone(       "test.com", "example.com") -> false
var InZone = func(domain, zone string) bool {
	return domain == zone || strings.HasSuffix(domain, "."+zone)
}
