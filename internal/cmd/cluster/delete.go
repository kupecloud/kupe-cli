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

			if err := waitForGone(cmd.Context(), io, api, name, "cluster "+name+" to be deleted", opts.waitTimeout); err != nil {
				return mapWaitErr(err)
			}
			fmt.Fprintf(io.Out, "cluster/%s deleted\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the cluster to disappear before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 10*time.Minute, "Give up after this long")
	return cmd
}
