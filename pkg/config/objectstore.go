package config

type ObjectStoreType string

const (
	ObjectStoreType_DISABLED ObjectStoreType = "disabled"
	ObjectStoreType_CONSOLE  ObjectStoreType = "stdout"
	ObjectStoreType_QPOINT   ObjectStoreType = "qpoint"
	ObjectStoreType_S3       ObjectStoreType = "s3"
)

type ServiceObjectStore struct {
	Type              ObjectStoreType `yaml:"type" validate:"required"`
	ID                string          `yaml:"id"`
	ObjectStoreConfig `yaml:",inline,omitempty"`
}

func (s ServiceObjectStore) ServiceType() string {
	switch s.Type {
	case ObjectStoreType_QPOINT:
		return "objectstore.warehouse"
	case ObjectStoreType_S3:
		return "objectstore.s3"
	case ObjectStoreType_CONSOLE:
		return "objectstore.console"
	case ObjectStoreType_DISABLED:
		return "objectstore.noop"
	case "e2e": // TODO(e2e)
		return "objectstore.e2e"
	default:
		return "objectstore.console"
	}
}

type ObjectStoreConfig struct {
	ObjectStoreQPointWarehouseConfig `yaml:",inline,omitempty"`
	ObjectStoreS3Config              `yaml:",inline,omitempty"`
	EventStore                       EventStoreSelector `yaml:"event_store"`
}

type ObjectStoreQPointWarehouseConfig struct {
	URL   string      `yaml:"url"`
	Token ValueSource `yaml:"token"`
}

type ObjectStoreS3Config struct {
	Endpoint  string      `yaml:"endpoint"`
	Bucket    string      `yaml:"bucket"`
	Region    string      `yaml:"region"`
	AccessKey ValueSource `yaml:"access_key"`
	SecretKey ValueSource `yaml:"secret_key"`
	AccessURL string      `yaml:"access_url"`
	Insecure  bool        `yaml:"insecure"`
}

type EventStoreSelector struct {
	ID       string `yaml:"id"`
	Disabled bool   `yaml:"disabled"`
}

type ObjectStoreSelector struct {
	ID       string `yaml:"id"`
	Disabled bool   `yaml:"disabled"`
}
