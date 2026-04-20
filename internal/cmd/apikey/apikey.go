// Package apikey wires the "kupe apikey" subcommand tree: list, create,
// delete. These endpoints are admin-only server-side — readonly callers
// receive 403, which maps to exit 3.
package apikey

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// NewCmd returns the parent apikey command with every v1 subcommand wired in.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apikey",
		Short: "Mint, list, and revoke tenant-scoped API keys",
		Long: `Manage API keys used for programmatic access to kupe-api.

Keys carry a role (admin or readonly) and optionally expire. The raw
` + "`kupe_...`" + ` token is only returned at creation time — store it
securely; it cannot be retrieved later.

All apikey operations require admin role; a readonly caller will exit
with code 3.`,
	}

	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newCreateCmd(f))
	cmd.AddCommand(newDeleteCmd(f))
	return cmd
}
