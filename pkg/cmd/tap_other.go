//go:build !linux

package cmd

import (
	"go.uber.org/zap"
)

func runTapCmd(logger *zap.Logger) {
	logger.Warn("'tap' leverages eBPF probes which can only run on Linux.")
}
