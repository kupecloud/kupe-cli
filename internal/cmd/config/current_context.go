package config

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newCurrentContextCmd(f *cli.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "current-context",
		Short: "Print the name of the current context",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}
			if cfg.CurrentContext == "" {
				return cli.NotFoundError("no current context set")
			}
			_, err = fmt.Fprintln(f.IOStreams.Out, cfg.CurrentContext)
			return err
		},
	}
}
