package config

type QscanType string

const (
	QscanType_DISABLED QscanType = "disabled"
	QscanType_CONSOLE  QscanType = "stdout"
	QscanType_Client   QscanType = "client"
)

type ServiceQscan struct {
	Type  QscanType   `yaml:"type" validate:"required"`
	URL   string      `yaml:"url"`
	Token ValueSource `yaml:"token"`
}

func (s ServiceQscan) ServiceType() string {
	switch s.Type {
	case QscanType_CONSOLE:
		return "qscan.console"
	case QscanType_Client:
		return "qscan.client"
	case QscanType_DISABLED:
		return "qscan.noop"
	default:
		return "qscan.console"
	}
}
