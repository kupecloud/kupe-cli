// Package auth wires the "kupe auth" subcommand tree: login, logout,
// whoami (Phase 2), get-token (Phase 4).
package auth

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// NewCmd returns the parent auth command with login and logout wired in.
// whoami and get-token land in later phases (see IMPLEMENTATION.md).
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate against Kupe and manage stored credentials",
		Long: `auth manages the credentials the CLI uses to talk to the Kupe API.

Run "kupe auth login" to store a tenant-scoped API token for the current
context. Tokens are stored in the OS keyring (Keychain on macOS, Secret
Service on Linux, Credential Manager on Windows) or in a plaintext
credentials file when no keyring is available.`,
	}

	cmd.AddCommand(newLoginCmd(f))
	cmd.AddCommand(newLogoutCmd(f))
	cmd.AddCommand(newWhoamiCmd(f))
	cmd.AddCommand(newGetTokenCmd(f))
	return cmd
}
