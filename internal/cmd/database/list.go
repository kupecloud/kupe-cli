package database

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	return service.ListCmd(f, cmdutil.Help{
		Short: "List databases in the tenant",
		Example: `  kupe database list
  kupe db list -o wide
  kupe db list -o name
  kupe db list --phase degraded`,
	})
}
