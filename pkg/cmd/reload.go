package cmd

import (
	"errors"
	"fmt"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := initLogger()
		if err != nil {
			return err
		}
		defer syncLogger(logger)

		return runReloadCmd(logger)
	},
}

func runReloadCmd(logger *zap.Logger) error {
	pid, err := findPIDByBinaryName(binaryName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("could not find a running qtap process")
		}
		return fmt.Errorf("find running qtap process: %w", err)
	}

	logger.Info("sending SIGHUP signal to process", zap.Int("pid", pid))
	err = syscall.Kill(pid, syscall.SIGHUP)
	if err != nil {
		return fmt.Errorf("send SIGHUP to process %d: %w", pid, err)
	}
	return nil
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
