package config

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newUseContextCmd(f *cli.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "use-context NAME",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}
			if cfg.Context(name) == nil {
				return cli.NotFoundError(fmt.Sprintf("context %q not found", name))
			}
			cfg.CurrentContext = name
			path, err := f.ConfigPath()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "resolving config path", err)
			}
			if err := cfg.Save(path); err != nil {
				return cli.Wrap(cli.ExitGeneral, "saving config", err)
			}
			fmt.Fprintf(f.IOStreams.ErrOut, "Switched to context %q (tenant %s).\n", name, cfg.Context(name).Tenant)
			return nil
		},
	}
}
