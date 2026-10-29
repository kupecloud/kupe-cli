package secret

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type createOpts struct {
	path        string
	sync        []string
	syncKeys    []string
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a secret definition",
		Long: `Register a secret definition at a Vault path and choose the cluster
namespaces that receive its values. Values must already exist at the path.

By default, wait until the current values reach every target namespace.
Missing values or selected keys can be fixed while the command waits. A
wait timeout leaves the definition in place and delivery continues.
Pass --wait=false to return once the definition is created.

Seed the values with kupe secret values set NAME --expected-version 0
--file values.json before adding sync targets. Values must exist before
the operator can mirror them.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Path only, no sync targets
  kupe secret create mydb-password --path apps/shop/api

  # Sync to one cluster's default namespace
  kupe secret create mydb-password --path apps/shop/api --sync prod:default

  # Sync to two clusters, custom target secret name in each
  kupe secret create api-key \
    --path apps/shop/api \
    --sync prod:default:upstream-api-key \
    --sync staging:default:upstream-api-key`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			targets, err := parseSyncTargets(opts.sync)
			if err != nil {
				return cli.MisuseError(err.Error())
			}
			if _, err := selectKeys(targets, opts.syncKeys, nil); err != nil {
				return cli.MisuseError(err.Error())
			}
			if opts.path == "" {
				return cli.MisuseError("--path is required")
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			s, _, err := api.CreateSecret(cmd.Context(), client.CreateSecretRequest{
				Name:       name,
				SecretPath: opts.path,
				Sync:       targets,
			})
			if err != nil {
				return err
			}
			if opts.wait {
				final, werr := waitForDelivery(cmd.Context(), f.IOStreams, api, name, opts.waitTimeout)
				if werr != nil {
					return secretWaitErr(werr, name, false)
				}
				s = final
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, s)
		},
	}

	cmd.Flags().StringVar(&opts.path, "path", "", "Vault path containing the values, e.g. apps/shop/api (required)")
	cmd.Flags().StringArrayVar(&opts.sync, "sync", nil, "Sync target as cluster:namespace[:secretName]; repeatable")
	cmd.Flags().StringArrayVar(&opts.syncKeys, "sync-key", nil, "Selected key as cluster:namespace:key; repeatable")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the secret is delivered before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
