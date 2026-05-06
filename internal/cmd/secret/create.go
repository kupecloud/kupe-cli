package secret

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type createOpts struct {
	path   string
	sync   []string
	output string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new managed secret",
		Long: `Register a managed secret. The CLI does NOT carry the secret value —
it only registers a pointer to a path in the platform secret store, and
optionally one or more sync targets (cluster + namespace pairs where the
operator will mirror the secret into a Kubernetes Secret).

Seed the value at the referenced path through the platform's secret-store
UI before adding sync targets, otherwise the mirrored Secrets will sit in
the Pending phase until the value exists.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Path only, no sync targets (seed values later)
  kupe secret create mydb-password --path kv/acme/mydb-password

  # Sync to one cluster's default namespace
  kupe secret create mydb-password --path kv/acme/mydb-password --sync prod:default

  # Sync to two clusters, custom target secret name in each
  kupe secret create api-key \
    --path kv/acme/api-key \
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
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, s)
		},
	}

	cmd.Flags().StringVar(&opts.path, "path", "", "Path in the platform secret store where the value lives, e.g. kv/acme/db-password (required)")
	cmd.Flags().StringArrayVar(&opts.sync, "sync", nil, "Sync target as cluster:namespace[:secretName]; repeatable")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
