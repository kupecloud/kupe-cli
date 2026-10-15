package database

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newWaitCmd(f *cli.Factory) *cobra.Command {
	return service.WaitCmd(f, cmdutil.Help{
		Short: "Block until a database reaches a target phase",
		Long: `Wait until a database reaches a target phase. Useful in scripts after
"kupe database create --wait=false" or "kupe database delete --wait=false",
or to resume watching after an interrupted wait.

Accepted --for values: running, pending, provisioning, upgrading,
degraded, terminating, or deleted (waits for the database to disappear).`,
		Example: `  kupe database wait orders-db --for running --timeout 10m
  kupe db wait orders-db --for deleted`,
	})
}
