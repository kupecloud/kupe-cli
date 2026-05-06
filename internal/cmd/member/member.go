// Package member wires the "kupe member" subcommand tree: list, add,
// update (role), remove. Members are email-identified users with a tenant
// role (admin or readonly); the operator syncs them to Authentik groups.
package member

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// NewCmd returns the parent member command with every v1 subcommand wired in.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "member",
		Short: "Manage tenant members",
		Long: `List, add, update, and remove users from the current tenant. Members
are identified by email and carry a role (admin or readonly).

Changes propagate to Authentik groups via the kupe-control-operator.`,
	}
	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newAddCmd(f))
	cmd.AddCommand(newUpdateCmd(f))
	cmd.AddCommand(newRemoveCmd(f))
	return cmd
}

// renderOne writes a single member.
func renderOne(out io.Writer, format *printer.Format, m *client.Member) error {
	return printer.RenderOne(out, format, m, printer.MemberColumns(), func(m *client.Member) string { return m.Email })
}

// renderList writes a slice of members.
func renderList(out io.Writer, format *printer.Format, ms []client.Member) error {
	return printer.RenderList(out, format, ms, printer.MemberColumns(), func(m client.Member) string { return m.Email })
}

// validRole reports whether a role string is one the API accepts.
func validRole(role string) bool {
	return role == client.RoleAdmin || role == client.RoleReadonly
}
