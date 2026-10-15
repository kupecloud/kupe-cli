package database

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newGetCmd(f *cli.Factory) *cobra.Command {
	return service.GetCmd(f, cmdutil.Help{
		Short: "Show the details of a single database",
		Example: `  kupe database get orders-db
  kupe db get orders-db -o yaml`,
	})
}
