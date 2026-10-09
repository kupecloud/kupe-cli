package database

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// majorVersion matches an engine major such as "18", the only form the
// API accepts for a version.
var majorVersion = regexp.MustCompile(`^[1-9]\d$`)

type upgradeOpts struct {
	version string
	output  string
}

func newUpgradeCmd(f *cli.Factory) *cobra.Command {
	opts := &upgradeOpts{}

	cmd := &cobra.Command{
		Use:   "upgrade NAME --version MAJOR",
		Short: "Upgrade a database to a newer major version",
		Long: `Request an in-place upgrade of a database to a newer engine major version.
Upgrades go upward only, and the database is offline while the upgrade runs.

Major version upgrades are not available yet. The command sends the request
so that it starts working as soon as the platform allows it; until then the
platform refuses it and the CLI shows the platform's reason unchanged.`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe database upgrade orders-db --version 18`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !majorVersion.MatchString(opts.version) {
				return cli.MisuseError(fmt.Sprintf("--version must be a major version such as 18; got %q", opts.version))
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}

			fmt.Fprintln(f.IOStreams.ErrOut, "Note: major version upgrades are not available yet; the platform may refuse this request.")
			updated, err := api.UpdateDatabaseRMW(cmd.Context(), name, func(current *client.Database) *client.PatchDatabaseRequest {
				if current.Version == opts.version {
					return nil
				}
				return &client.PatchDatabaseRequest{Version: &opts.version}
			})
			if err != nil {
				return translateUpgradeErr(err)
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, updated)
		},
	}

	cmd.Flags().StringVar(&opts.version, "version", "", "Target engine major version (e.g. 18) (required)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
