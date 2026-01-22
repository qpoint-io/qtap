package config

type TrafficDirection string

var (
	TrafficDirection_ALL             TrafficDirection = "all"
	TrafficDirection_INGRESS         TrafficDirection = "ingress"
	TrafficDirection_EGRESS          TrafficDirection = "egress"
	TrafficDirection_EGRESS_INTERNAL TrafficDirection = "egress-internal"
	TrafficDirection_EGRESS_EXTERNAL TrafficDirection = "egress-external"
)

type TapProtocolConfig struct {
	Stack string `yaml:"stack"`
}

func (c *TapProtocolConfig) HasStack() bool {
	return c.Stack != "" && c.Stack != "none"
}

type TapEndpointConfig struct {
	Domain string            `yaml:"domain" validate:"required,hostname"`
	Http   TapProtocolConfig `yaml:"http"`
	Redis  TapProtocolConfig `yaml:"redis"`
}

type TapConfig struct {
	Direction       TrafficDirection    `yaml:"direction"`
	IgnoreLoopback  bool                `yaml:"ignore_loopback"`
	AuditIncludeDNS bool                `yaml:"audit_include_dns"`
	Http            TapProtocolConfig   `yaml:"http"`
	Redis           TapProtocolConfig   `yaml:"redis"`
	Filters         TapFilters          `yaml:"filters,omitempty"`
	Endpoints       []TapEndpointConfig `yaml:"endpoints" validate:"dive"`
}

func (c *TapConfig) HasAnyStack() bool {
	if c.Http.HasStack() || c.Redis.HasStack() {
		return true
	}

	for _, e := range c.Endpoints {
		if e.Http.HasStack() || e.Redis.HasStack() {
			return true
		}
	}

	return false
}
