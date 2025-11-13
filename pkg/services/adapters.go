package services

import (
	"go.uber.org/zap"
)

// LoggerAdapter is a service that accepts a logger
type LoggerAdapter interface {
	SetLogger(*zap.Logger)
}

type LogHelper struct {
	logger *zap.Logger
}

func (l *LogHelper) SetLogger(logger *zap.Logger) {
	l.logger = logger
}

func (l *LogHelper) Log() *zap.Logger {
	if l.logger == nil {
		l.logger = zap.NewNop()
	}

	return l.logger
}
