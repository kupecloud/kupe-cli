package database

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	cpu                string
	memory             string
	storage            string
	highAvailability   bool
	deletionProtection bool
	output             string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Resize a database or change its availability or protection",
		Long: `Change a database's per-instance resources, high availability, or deletion
protection. Only the flags you pass are changed.

Changing CPU or memory rolls the instances (a highly available database
switches over first). Storage grows online and can never shrink. To change
the major version, use "kupe database upgrade".

Concurrent updates from another client are detected and retried once
automatically. Changes apply asynchronously; follow them with
"kupe database get NAME".`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database update orders-db --cpu 1 --memory 2Gi
  kupe db update orders-db --storage 20Gi
  kupe db update orders-db --ha
  kupe db update orders-db --deletion-protection=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			patch := client.PatchDatabaseRequest{}
			if opts.cpu != "" || opts.memory != "" || opts.storage != "" {
				if err := validateResources(opts.cpu, opts.memory, opts.storage); err != nil {
					return err
				}
				patch.Resources = &client.DatabaseResources{CPU: opts.cpu, Memory: opts.memory, Storage: opts.storage}
			}
			if cmd.Flags().Changed("ha") {
				patch.HighAvailability = &opts.highAvailability
			}
			if cmd.Flags().Changed("deletion-protection") {
				patch.DeletionProtection = &opts.deletionProtection
			}
			if patch == (client.PatchDatabaseRequest{}) {
				return cli.MisuseError("nothing to update: pass at least one of --cpu, --memory, --storage, --ha, --deletion-protection")
			}

			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			updated, err := api.UpdateDatabaseRMW(cmd.Context(), name, func(*client.Database) *client.PatchDatabaseRequest {
				return &patch
			})
			if err != nil {
				return translateErr(err)
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, updated)
		},
	}

	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "New CPU per instance (e.g. 1, 500m)")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "New memory per instance (e.g. 2Gi)")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "New storage per instance (e.g. 20Gi); grow only")
	cmd.Flags().BoolVar(&opts.highAvailability, "ha", false, "Turn high availability on (--ha) or off (--ha=false)")
	cmd.Flags().BoolVar(&opts.deletionProtection, "deletion-protection", false, "Turn deletion protection on (--deletion-protection) or off (--deletion-protection=false)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
