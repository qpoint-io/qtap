package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [config-file]",
	Short: "Validate a qtap config file",
	Long: `Validate a qtap configuration file without starting the agent.
Pass '-' to read from stdin.

Example usage:
  qtap validate /etc/qtap/config.yaml
  cat config.yaml | qtap validate -`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var data []byte
		var err error

		if args[0] == "-" {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		} else {
			data, err = os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading config file: %w", err)
			}
		}

		cfg, err := config.UnmarshalConfig(data)
		if err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}

		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("validating config: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Config is valid.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
