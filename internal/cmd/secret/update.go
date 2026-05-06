package secret

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	sync   []string
	output string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update a managed secret's sync targets",
		Long: `Replace the set of sync targets for a managed secret. Pass --sync
once per target; the value passed REPLACES the existing list rather than
appending to it.

Omitting --sync leaves the secret untouched. To clear all targets, use
"kupe secret delete NAME" and recreate without --sync.

Concurrent updates from another client are detected and retried once
automatically.`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe secret update mydb-password --sync prod:default --sync staging:default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			if len(opts.sync) == 0 {
				return cli.MisuseError("--sync is required (at least once); to remove a target use \"kupe secret delete\" + recreate")
			}
			targets, err := parseSyncTargets(opts.sync)
			if err != nil {
				return cli.MisuseError(err.Error())
			}

			api, err := f.Client()
			if err != nil {
				return err
			}
			updated, err := api.UpdateSecretRMW(cmd.Context(), name, func(_ *client.Secret) *client.PatchSecretRequest {
				return &client.PatchSecretRequest{Sync: targets}
			})
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, updated)
		},
	}

	cmd.Flags().StringArrayVar(&opts.sync, "sync", nil, "Sync target as cluster:namespace[:secretName]; repeatable; replaces the full list")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
