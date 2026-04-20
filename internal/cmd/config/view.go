package config

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newViewCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Print the full config (tokens redacted)",
		Long: `Render the parsed config file. Tokens are never included in the output —
tokenRef shows where each context's token lives (keyring or plaintext) but
the token value itself is retrieved only at command-run time.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return cli.Wrap(cli.ExitGeneral, "loading config", err)
			}
			switch output {
			case "", "yaml":
				data, err := yaml.Marshal(cfg)
				if err != nil {
					return cli.Wrap(cli.ExitGeneral, "marshalling config", err)
				}
				_, err = f.IOStreams.Out.Write(data)
				return err
			case "json":
				enc := json.NewEncoder(f.IOStreams.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(cfg)
			default:
				return cli.MisuseError(fmt.Sprintf("unsupported output format %q (expected yaml or json)", output))
			}
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: yaml (default) or json")
	return cmd
}
