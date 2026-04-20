// Package invoice wires the "kupe invoice" subcommand tree — list + get.
// Invoices are read-only server-side (GET-only endpoints); both admin and
// readonly roles can call them.
package invoice

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// NewCmd returns the parent invoice command.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invoice",
		Short: "List and inspect tenant invoices",
		Long: `Read-only access to the tenant's billing history. Invoices are named by
billing period ("2026-03"); -o json surfaces the full line-item breakdown
for downstream scripts or spreadsheets.`,
	}
	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newGetCmd(f))
	return cmd
}

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List invoices for the current tenant",
		Example: `  kupe invoice list
  kupe invoice list -o wide
  kupe invoice list -o json | jq '.[] | select(.status.phase=="Open")'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListInvoices(cmd.Context())
			if err != nil {
				return err
			}
			return renderList(f.IOStreams.Out, format, list)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|wide|json|yaml|name|go-template=...")
	return cmd
}

func newGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get PERIOD",
		Short: "Show one invoice (billing period, e.g. 2026-03)",
		Args:  cobra.ExactArgs(1),
		Example: `  kupe invoice get 2026-03
  kupe invoice get 2026-03 -o json | jq .status.lineItems`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			inv, err := api.GetInvoice(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, format, inv)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|json|yaml|go-template=...")
	return cmd
}

func parsedFormat(f *cli.Factory, raw string) (*printer.Format, error) {
	if raw == "" {
		raw = f.DefaultOutput()
	}
	return printer.MustParse(raw)
}

func renderOne(out io.Writer, format *printer.Format, inv *client.Invoice) error {
	return printer.RenderOne(out, format, inv, printer.InvoiceDetailColumns(), func(inv *client.Invoice) string { return inv.Name })
}

func renderList(out io.Writer, format *printer.Format, list []client.Invoice) error {
	return printer.RenderList(out, format, list, printer.InvoiceColumns(), func(inv client.Invoice) string { return inv.Name })
}
