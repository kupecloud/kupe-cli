package cluster

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newGetCmd(f *cli.Factory) *cobra.Command {
	return service.GetCmd(f, cmdutil.Help{
		Short: "Show the details of a single cluster",
		Example: `  kupe cluster get prod
  kupe cluster get prod -o yaml`,
	})
}
