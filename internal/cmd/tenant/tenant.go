// Package tenant wires the "kupe tenant" subcommand tree: `get` — the
// current tenant's full record including plan, phase, resource pool,
// current-period usage, and member list — and `delete`, the owner-only
// typed-name deletion. `update` (PATCH) may land later; for now the
// authoritative write path for everything else is the console.
package tenant

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// NewCmd returns the parent tenant command.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Inspect or delete the current tenant",
		Long: `Read full details for the tenant the current context targets: plan,
phase, resource pool, current-period usage, and member list — or, as the
tenant owner, delete it.

Unlike "kupe auth whoami" — a quick identity check — "kupe tenant get"
surfaces every field the API returns so billing, capacity, and membership
questions can be answered in one call.`,
	}
	cmd.AddCommand(newGetCmd(f))
	cmd.AddCommand(newDeleteCmd(f))
	return cmd
}

func newGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show full details of the current tenant",
		Example: `  kupe tenant get
  kupe tenant get -o yaml
  kupe tenant get -o json | jq .status.billing.currentUsage`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			t, _, err := api.GetTenant(cmd.Context())
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, t)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func renderOne(out io.Writer, colorEnabled bool, format *printer.Format, t *client.Tenant) error {
	return printer.RenderOne(out, format, t, printer.TenantDetailColumns(colorEnabled), func(t *client.Tenant) string { return t.Name })
}
