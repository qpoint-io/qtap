package config

import "slices"

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
	Domain   string            `yaml:"domain" validate:"required,hostname"`
	Http     TapProtocolConfig `yaml:"http"`
	Redis    TapProtocolConfig `yaml:"redis"`
	MySQL    TapProtocolConfig `yaml:"mysql"`
	Postgres TapProtocolConfig `yaml:"postgres"`
}

type TapConfig struct {
	Direction       TrafficDirection    `yaml:"direction"`
	IgnoreLoopback  bool                `yaml:"ignore_loopback"`
	AuditIncludeDNS bool                `yaml:"audit_include_dns"`
	Http            TapProtocolConfig   `yaml:"http"`
	Redis           TapProtocolConfig   `yaml:"redis"`
	MySQL           TapProtocolConfig   `yaml:"mysql"`
	Postgres        TapProtocolConfig   `yaml:"postgres"`
	Filters         TapFilters          `yaml:"filters,omitempty"`
	Endpoints       []TapEndpointConfig `yaml:"endpoints" validate:"dive"`
}

func (c *TapConfig) HasAnyStack() bool {
	if c.Http.HasStack() || c.Redis.HasStack() || c.MySQL.HasStack() || c.Postgres.HasStack() {
		return true
	}

	for _, e := range c.Endpoints {
		if e.Http.HasStack() || e.Redis.HasStack() || e.MySQL.HasStack() || e.Postgres.HasStack() {
			return true
		}
	}

	return false
}

func (c *TapConfig) GetAllProtocols() []string {
	protocols := []string{}
	if c.Http.HasStack() {
		protocols = append(protocols, "http1")
		protocols = append(protocols, "http2")
	}
	if c.Redis.HasStack() {
		protocols = append(protocols, "redis")
	}
	if c.MySQL.HasStack() {
		protocols = append(protocols, "mysql")
	}
	if c.Postgres.HasStack() {
		protocols = append(protocols, "postgres")
	}

	for _, e := range c.Endpoints {
		if e.Http.HasStack() {
			protocols = append(protocols, "http1")
			protocols = append(protocols, "http2")
		}
		if e.Redis.HasStack() {
			protocols = append(protocols, "redis")
		}
		if e.MySQL.HasStack() {
			protocols = append(protocols, "mysql")
		}
		if e.Postgres.HasStack() {
			protocols = append(protocols, "postgres")
		}
	}

	// remove duplicates
	return slices.Compact(protocols)
}
