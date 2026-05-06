package cluster

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
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

			normalised := normalizePhase(opts.forPhase)
			switch normalised {
			case "Deleted":
				if err := waitForGone(cmd.Context(), f.IOStreams, api, name, "cluster "+name+" to be deleted", opts.timeout); err != nil {
					return mapWaitErr(err)
				}
				fmt.Fprintf(f.IOStreams.Out, "cluster/%s deleted\n", name)
				return nil
			case "":
				return cli.MisuseError(fmt.Sprintf("unsupported --for value %q", opts.forPhase))
			}
			final, werr := waitForPhase(cmd.Context(), f.IOStreams, api, name, "cluster "+name, normalised, opts.timeout)
			if werr != nil {
				return mapWaitErr(werr)
			}
			fmt.Fprintf(f.IOStreams.Out, "cluster/%s %s\n", name, strings.ToLower(normalised))
			_ = final
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.forPhase, "for", "running", "Target phase to wait for: running | pending | provisioning | upgrading | degraded | terminating | deleted")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 30*time.Minute, "Give up after this long")
	return cmd
}

// normalizePhase maps friendly short forms onto the server-side phase
// constants. Returns the empty string for unknowns so the caller can
// distinguish "invalid" from the special "Deleted" sentinel.
func normalizePhase(s string) string {
	switch strings.ToLower(s) {
	case "running":
		return client.PhaseRunning
	case "deleted", "gone":
		return "Deleted"
	case "pending":
		return client.PhasePending
	case "provisioning":
		return client.PhaseProvisioning
	case "upgrading":
		return client.PhaseUpgrading
	case "degraded":
		return client.PhaseDegraded
	case "terminating":
		return client.PhaseTerminating
	}
	return ""
}
