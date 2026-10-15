package cluster

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	return service.ListCmd(f, cmdutil.Help{
		Short: "List clusters in the current tenant",
		Long: `List every cluster visible to the current tenant.

Output defaults to a human table; use -o name for pipe-friendly output,
or -o json / -o yaml / -o go-template=... for machine consumption.`,
		Example: `  kupe cluster list
  kupe cluster list -o wide
  kupe cluster list -o name | xargs -I{} kupe cluster delete {} --yes
  kupe cluster list --phase Running`,
	})
}
