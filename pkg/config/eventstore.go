package config

type EventStoreType string

const (
	EventStoreType_DISABLED EventStoreType = "disabled"
	EventStoreType_CONSOLE  EventStoreType = "stdout"
	EventStoreType_PULSE    EventStoreType = "pulse"
	EventStoreType_AXIOM    EventStoreType = "axiom"
	EventStoreType_OTEL     EventStoreType = "otel"
)

type ServiceEventStore struct {
	Type             EventStoreType `yaml:"type" validate:"required"`
	ID               string         `yaml:"id"`
	EventStoreConfig `yaml:",inline,omitempty"`
}

func (s ServiceEventStore) ServiceType() string {
	switch s.Type {
	case EventStoreType_PULSE:
		return "eventstore.eventstorev1"
	case EventStoreType_CONSOLE:
		return "eventstore.console"
	case EventStoreType_DISABLED:
		return "eventstore.noop"
	case EventStoreType_AXIOM:
		return "eventstore.axiom"
	case EventStoreType_OTEL:
		return "eventstore.otel"
	case "e2e": // TODO(e2e)
		return "eventstore.e2e"
	default:
		return "eventstore.console"
	}
}

type EventStoreConfig struct {
	Token                 ValueSource `yaml:"token"`
	EventStorePulseConfig `yaml:",inline,omitempty"`
	EventStoreAxiomConfig `yaml:",inline,omitempty"`
	EventStoreOTelConfig  `yaml:",inline,omitempty"`
}

type EventStorePulseConfig struct {
	URL           string `yaml:"url"`
	AllowInsecure bool   `yaml:"allow_insecure"`
}

type EventStoreAxiomConfig struct {
	Dataset ValueSource `yaml:"dataset"`
}

type EventStoreOTelConfig struct {
	Endpoint    string                 `yaml:"endpoint"`
	Protocol    string                 `yaml:"protocol"` // "grpc", "http", "stdout"
	Headers     map[string]ValueSource `yaml:"headers"`  // Optional headers for auth
	ServiceName string                 `yaml:"service_name"`
	Environment string                 `yaml:"environment"`
	TLS         EventStoreOTelTLS      `yaml:"tls"`
}

type EventStoreOTelTLS struct {
	Enabled            bool `yaml:"enabled"`
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}
