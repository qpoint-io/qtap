package config

import "os"

type ValueSourceType string

const (
	ValueSourceType_ENV  ValueSourceType = "env"
	ValueSourceType_TEXT ValueSourceType = "text"
)

type ValueSource struct {
	Type  ValueSourceType `yaml:"type"`
	Value string          `yaml:"value"`
}

func (vs ValueSource) String() string {
	switch vs.Type {
	case ValueSourceType_ENV:
		return os.Getenv(vs.Value)
	case ValueSourceType_TEXT:
		return vs.Value
	default:
		return ""
	}
}

type Cert struct {
	Ca  string `yaml:"ca" validate:"required,stringnotempty"`
	Crt string `yaml:"crt" validate:"required,stringnotempty"`
	Key string `yaml:"key" validate:"required,stringnotempty"`
}
