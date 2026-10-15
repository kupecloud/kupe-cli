package cluster

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newWaitCmd(f *cli.Factory) *cobra.Command {
	return service.WaitCmd(f, cmdutil.Help{
		Short: "Block until a cluster reaches a target phase",
		Long: `Wait until a cluster reaches a target phase. Useful in CI scripts after
a --wait=false create/update/delete, or to resume watching after a
cancelled long-op.

Accepted --for values: running, pending, provisioning, upgrading,
degraded, terminating, or deleted (waits for the cluster to disappear).`,
		Example: `  kupe cluster wait prod --for running --timeout 10m
  kupe cluster wait prod --for deleted`,
	})
}
