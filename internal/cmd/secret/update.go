package secret

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	sync        []string
	syncKeys    []string
	allKeys     []string
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update a secret's sync targets",
		Long: `Replace a secret's sync targets. Pass --sync once per target.
A retained target keeps its selected keys unless --sync-key replaces them
or --all-keys selects the whole entry. Each key flag must name a target
included with --sync in the same command.

By default, wait until the changed definition is delivered. Pass --wait=false
to return once the update is accepted. At least one --sync is required;
to clear every target, delete the definition and recreate it without --sync.

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

			explicit, err := selectKeys(targets, opts.syncKeys, opts.allKeys)
			if err != nil {
				return cli.MisuseError(err.Error())
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			var selectionErr error
			updated, err := api.UpdateSecretRMW(cmd.Context(), name, func(current *client.Secret) *client.PatchSecretRequest {
				sync := keepTargetKeys(targets, current, explicit)
				total := 0
				for _, target := range sync {
					total += len(target.Keys)
				}
				if total > maxSelectedKeys {
					selectionErr = cli.MisuseError("a secret accepts at most 1,024 selected keys")
					return nil
				}
				selectionErr = nil
				return &client.PatchSecretRequest{Sync: sync}
			})
			if selectionErr != nil {
				return selectionErr
			}
			if err != nil {
				return err
			}
			if opts.wait {
				final, werr := waitForDelivery(cmd.Context(), f.IOStreams, api, name, opts.waitTimeout)
				if werr != nil {
					return secretWaitErr(werr, name, false)
				}
				updated = final
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, updated)
		},
	}

	cmd.Flags().StringArrayVar(&opts.sync, "sync", nil, "Sync target as cluster:namespace[:secretName]; repeatable; replaces the full list")
	cmd.Flags().StringArrayVar(&opts.syncKeys, "sync-key", nil, "Selected key as cluster:namespace:key; repeatable")
	cmd.Flags().StringArrayVar(&opts.allKeys, "all-keys", nil, "Send the whole entry to cluster:namespace; repeatable")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the secret is delivered before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
