package cmd

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const binaryName = "qtap"

var reloadConfigCmd = &cobra.Command{
	Use:   "reload-config",
	Short: "Live reload the current config",
	Long: `Reload the current configuration without restarting the application.
Example usage:
  qtap reload-config`,
	Run: func(cmd *cobra.Command, args []string) {
		logger := initLogger()
		defer syncLogger(logger)

		runReloadCmd(logger)
	},
}

func runReloadCmd(logger *zap.Logger) {
	pid, err := findPIDByBinaryName(binaryName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Fatal("could not find a running qtap process")
		}
		logger.Fatal("error finding running qtap process", zap.Error(err))
	}

	logger.Info("sending SIGHUP signal to process", zap.Int("pid", pid))
	err = syscall.Kill(pid, syscall.SIGHUP)
	if err != nil {
		logger.Fatal("error sending signal", zap.Error(err))
	}
}

func findPIDByBinaryName(name string) (int, error) {
	currentPID := os.Getpid() // Get the current process PID
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, proc := range procs {
		if pid, err := strconv.Atoi(proc.Name()); err == nil {
			if pid == currentPID {
				continue // Skip the current process
			}
			cmdline, err := os.ReadFile("/proc/" + proc.Name() + "/cmdline")
			if err == nil {
				cmds := strings.Split(string(cmdline), "\x00")
				if len(cmds) > 0 && strings.Contains(cmds[0], name) {
					return pid, nil
				}
			}
		}
	}
	return 0, os.ErrNotExist
}
