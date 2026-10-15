package database

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	cpu                string
	memory             string
	storage            string
	highAvailability   bool
	deletionProtection bool
	wait               bool
	waitTimeout        time.Duration
	output             string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{wait: true, waitTimeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Resize a database or change its availability or protection",
		Long: `Change a database's per-instance resources, high availability, or deletion
protection. Only the flags you pass are changed.

Changing CPU or memory rolls the instances (a highly available database
switches over first). Storage grows online and can never shrink. The major
version cannot be changed yet.

Concurrent updates from another client are detected and retried once
automatically. By default the command waits until the change is in effect:
the platform has applied it and every instance runs it again. A database
with a single instance is briefly Degraded while that instance restarts for
a CPU or memory change; the wait rides that out, but fails if the database
stays Degraded for 5 minutes. A change to deletion protection alone is in
effect at once and is not waited on. Pass --wait=false to return as soon as
the change is accepted.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database update orders-db --cpu 1 --memory 2Gi
  kupe db update orders-db --storage 20Gi
  kupe db update orders-db --ha
  kupe db update orders-db --deletion-protection=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			patch := client.PatchDatabaseRequest{}
			if opts.cpu != "" || opts.memory != "" || opts.storage != "" {
				if err := validateResources(opts.cpu, opts.memory, opts.storage, false); err != nil {
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
			updated, err := api.Databases().UpdateRMW(cmd.Context(), name, func(*client.Database) *client.PatchDatabaseRequest {
				return &patch
			})
			if err != nil {
				return translateUpdateErr(err, name)
			}
			// Deletion protection is read from the spec when a delete comes
			// in, so it is in effect once the PATCH lands; only a resize or an
			// HA change rolls the instances. Waiting on a protection-only
			// change would hold the "turn protection off, then delete" path
			// on a database that is Degraded or was never provisioned.
			if !opts.wait || (patch.Resources == nil && patch.HighAvailability == nil) {
				return printer.Database.One(f.IOStreams, format, updated)
			}
			// The PATCH response carries the generation the change produced.
			final, werr := service.WaitForConverged(cmd.Context(), f.IOStreams, api, name, updated.Generation, opts.waitTimeout)
			if werr != nil {
				return cmdutil.MapWaitErr(werr, noun, name, "update")
			}
			if final == nil {
				final = updated
			}
			return printer.Database.One(f.IOStreams, format, final)
		},
	}

	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "New CPU per instance (e.g. 1, 500m)")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "New memory per instance (e.g. 2Gi)")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "New storage per instance (e.g. 20Gi); grow only")
	cmd.Flags().BoolVar(&opts.highAvailability, "ha", false, "Turn high availability on (--ha) or off (--ha=false)")
	cmd.Flags().BoolVar(&opts.deletionProtection, "deletion-protection", false, "Turn deletion protection on (--deletion-protection) or off (--deletion-protection=false)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the change is in effect before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
