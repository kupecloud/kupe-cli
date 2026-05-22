package cluster

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

type deleteOpts struct {
	yes         bool
	wait        bool
	waitTimeout time.Duration
}

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	opts := &deleteOpts{wait: true, waitTimeout: 10 * time.Minute}

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a cluster",
		Long: `Delete a cluster. Prompts on a TTY by default; pass --yes to skip
confirmation (required in non-interactive environments).

By default waits for the cluster to disappear (404 on GetCluster). Pass
--wait=false to return as soon as the delete request is accepted.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe cluster delete prod
  kupe cluster delete prod --yes --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams

			api, err := f.Client()
			if err != nil {
				return err
			}

			// Surface the destructive semantics before prompting so users
			// see what "delete cluster" actually does on the kupe platform.
			// Skipped along with the confirm prompt when --yes is passed:
			// IaC / automation callers have already decided to proceed and
			// don't want the warning text in their logs. The same contract
			// is reflected in the console's DeleteClusterDialog and the
			// terraform-provider-kupe kupe_cluster docs.
			if !opts.yes {
				printDeleteClusterWarning(io, name)
			}

			if err := cli.ConfirmDelete(io, opts.yes, "cluster", name); err != nil {
				return err
			}

			if err := api.DeleteCluster(cmd.Context(), name); err != nil {
				return err
			}

			if !opts.wait {
				fmt.Fprintf(io.Out, "cluster/%s delete requested\n", name)
				return nil
			}

			if err := waitForGone(cmd.Context(), io, api, name, "cluster "+name, opts.waitTimeout); err != nil {
				return mapWaitErr(err, name, "delete")
			}
			// No additional stdout line — the spinner/plain renderer's
			// "✓ cluster <name> deleted (00:12)" success line is the
			// single source of truth on the wait path.
			return nil
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the cluster to disappear before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 10*time.Minute, "Give up after this long")
	return cmd
}

// printDeleteClusterWarning writes the destructive-action warning that
// precedes the cluster delete confirmation. The text is the CLI mirror
// of the console's DeleteClusterDialog warning — keep them in sync so
// tenants see the same contract everywhere.
//
// Routed to ErrOut (stderr) so it stays out of any captured stdout
// payload from `kupe cluster delete | …`. Same stream the confirm
// prompt itself uses.
func printDeleteClusterWarning(io *cli.IOStreams, name string) {
	const warn = `
Deleting cluster %q will:
  - Stop and permanently remove every workload running inside the cluster,
    along with its storage.
  - Delete every Argo Application, alerting rule, and Grafana dashboard this
    cluster published to the platform — INCLUDING any workloads those
    Applications deployed to your OTHER Kupe clusters.
  - Remove the cluster's public DNS endpoint.

Anything you provisioned in third-party systems from inside this cluster
(cloud providers, SaaS, DNS zones you own) will NOT be cleaned up. Drain
those before deleting if you want them removed.

`
	fmt.Fprintf(io.ErrOut, warn, name)
}
