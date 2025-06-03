package config

type EventStoreType string

const (
	EventStoreType_DISABLED        EventStoreType = "disabled"
	EventStoreType_CONSOLE         EventStoreType = "stdout"
	EventStoreType_PULSE           EventStoreType = "pulse"
	EventStoreType_PULSE_STREAMING EventStoreType = "pulse-streaming"
	EventStoreType_PULSE_LEGACY    EventStoreType = "pulse-legacy"
	EventStoreType_AXIOM           EventStoreType = "axiom"
)

type ServiceEventStore struct {
	Type             EventStoreType `yaml:"type" validate:"required"`
	ID               string         `yaml:"id"`
	EventStoreConfig `yaml:",inline,omitempty"`
}

func (s ServiceEventStore) ServiceType() string {
	switch s.Type {
	case EventStoreType_PULSE:
		return "eventstore.eventstorev1_nonstreaming"
	case EventStoreType_PULSE_STREAMING:
		return "eventstore.eventstorev1"
	case EventStoreType_PULSE_LEGACY:
		return "eventstore.pulse"
	case EventStoreType_CONSOLE:
		return "eventstore.console"
	case EventStoreType_DISABLED:
		return "eventstore.noop"
	case EventStoreType_AXIOM:
		return "eventstore.axiom"
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
}

type EventStorePulseConfig struct {
	URL           string `yaml:"url"`
	AllowInsecure bool   `yaml:"allow_insecure"`
}

type EventStoreAxiomConfig struct {
	Dataset ValueSource `yaml:"dataset"`
}
