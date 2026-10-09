package cluster

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

type waitOpts struct {
	forPhase string
	timeout  time.Duration
}

func newWaitCmd(f *cli.Factory) *cobra.Command {
	opts := &waitOpts{forPhase: "running", timeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "wait NAME",
		Short: "Block until a cluster reaches a target phase",
		Long: `Wait until a cluster reaches a target phase. Useful in CI scripts after
a --wait=false create/update/delete, or to resume watching after a
cancelled long-op.

Accepted --for values: running, pending, provisioning, upgrading,
degraded, terminating, or deleted (waits for the cluster to disappear).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe cluster wait prod --for running --timeout 10m
  kupe cluster wait prod --for deleted`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			api, err := f.Client()
			if err != nil {
				return err
			}
			return cmdutil.RunWait(cmd.Context(), f.IOStreams, "cluster", name, opts.forPhase, phaseGetter(api, name), opts.timeout)
		},
	}

	cmd.Flags().StringVar(&opts.forPhase, "for", "running", "Target phase to wait for: running | pending | provisioning | upgrading | degraded | terminating | deleted")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 30*time.Minute, "Give up after this long")
	return cmd
}
