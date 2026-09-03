// Package user wires the "kupe user" subcommand tree — self-service
// operations on the logged-in user's own account. These go to the signup
// service (config.DefaultSignupURL), not kupe-api, and require an OIDC
// login: an API key identifies a tenant, not a person.
package user

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// NewCmd returns the parent user command.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage your own Kupe user account",
		Long: `Self-service operations on the account you are logged in as.

These talk to the Kupe signup service rather than the tenant API and need
an OIDC login ("kupe auth login"); API keys are refused because a key
identifies a tenant, not a person.`,
	}
	cmd.AddCommand(newDeleteCmd(f))
	return cmd
}
