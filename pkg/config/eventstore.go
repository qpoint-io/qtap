package config

type EventStoreType string

const (
	EventStoreType_DISABLED     EventStoreType = "disabled"
	EventStoreType_CONSOLE      EventStoreType = "stdout"
	EventStoreType_PULSE        EventStoreType = "pulse"
	EventStoreType_PULSE_LEGACY EventStoreType = "pulse-legacy"
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
	case EventStoreType_PULSE_LEGACY:
		return "eventstore.pulse"
	case EventStoreType_CONSOLE:
		return "eventstore.console"
	case EventStoreType_DISABLED:
		return "eventstore.noop"
	case "e2e": // TODO(e2e)
		return "eventstore.e2e"
	default:
		return "eventstore.console"
	}
}

type EventStoreConfig struct {
	EventStorePulseConfig `yaml:",inline,omitempty"`
}

type EventStorePulseConfig struct {
	URL           string      `yaml:"url"`
	Token         ValueSource `yaml:"token"`
	AllowInsecure bool        `yaml:"allow_insecure"`
}
