package database

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
	opts := &waitOpts{}

	cmd := &cobra.Command{
		Use:   "wait NAME",
		Short: "Block until a database reaches a target phase",
		Long: `Wait until a database reaches a target phase. Useful in scripts after
"kupe database create --wait=false" or "kupe database delete", or to resume
watching after an interrupted wait.

Accepted --for values: running, pending, provisioning, upgrading,
degraded, terminating, or deleted (waits for the database to disappear).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database wait orders-db --for running --timeout 10m
  kupe db wait orders-db --for deleted`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			api, err := f.Client()
			if err != nil {
				return err
			}
			return cmdutil.RunWait(cmd.Context(), f.IOStreams, noun, name, opts.forPhase, phaseGetter(api, name), opts.timeout)
		},
	}

	cmd.Flags().StringVar(&opts.forPhase, "for", "running", "Target phase to wait for: running | pending | provisioning | upgrading | degraded | terminating | deleted")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 30*time.Minute, "Give up after this long")
	return cmd
}
