// Package config wires the "kupe config" subcommand tree.
package config

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// NewCmd returns the parent config command with every v1 subcommand wired in.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and modify the kupe CLI config file",
		Long: `Read and write the config file at ~/.config/kupe/config.yaml
(or $XDG_CONFIG_HOME/kupe/, or %AppData%\kupe\).

Tokens are never stored in the config file — they live in the OS keyring
or, as a fallback, a separate credentials.yaml next to the config.`,
	}

	cmd.AddCommand(newViewCmd(f))
	cmd.AddCommand(newCurrentContextCmd(f))
	cmd.AddCommand(newUseContextCmd(f))
	cmd.AddCommand(newSetContextCmd(f))
	cmd.AddCommand(newDeleteContextCmd(f))
	cmd.AddCommand(newGetCmd(f))
	cmd.AddCommand(newSetCmd(f))
	return cmd
}
