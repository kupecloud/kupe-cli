package cluster

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	return service.DeleteCmd(f, cmdutil.Delete{
		Help: cmdutil.Help{
			Short: "Delete a cluster",
			Long: `Delete a cluster. Prompts on a TTY by default; pass --yes to skip
confirmation (required in non-interactive environments).

By default waits for the cluster to disappear (404 on GetCluster). Pass
--wait=false to return as soon as the delete request is accepted.`,
			Example: `  kupe cluster delete prod
  kupe cluster delete prod --yes --wait=false`,
		},
		// Surface the destructive semantics before prompting so users see
		// what "delete cluster" actually does on the kupe platform. The same
		// contract is reflected in the console's delete dialog and the
		// terraform-provider-kupe kupe_cluster docs.
		Warning:     printDeleteClusterWarning,
		WaitTimeout: 10 * time.Minute,
	})
}

// printDeleteClusterWarning writes the destructive-action warning that
// precedes the cluster delete confirmation. The text is the CLI mirror
// of the console's delete dialog warning — keep them in sync so tenants
// see the same contract everywhere.
//
// Routed to ErrOut (stderr) so it stays out of any captured stdout
// payload from `kupe cluster delete | …`. Same stream the confirm
// prompt itself uses.
func printDeleteClusterWarning(io *cli.IOStreams, name string) {
	const warn = `
Deleting cluster %q will:
  - Permanently remove every workload running inside the cluster, along
    with its storage.
  - Remove the cluster's public DNS endpoint.

`
	fmt.Fprintf(io.ErrOut, warn, name)
}
