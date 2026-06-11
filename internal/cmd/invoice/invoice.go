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
		Long: `Read-only access to the tenant's billing history. Invoices are billed in
arrears: usage charges and the plan fee for a period land on the same
invoice once the period closes. Names are server-controlled: usually
"{tenant}-{YYYYMMDD}" (period start date, e.g. "acme-20260301"), but
variants exist — a final invoice issued on cancellation or deletion
carries a "-final" suffix, and a timestamp-suffixed form is used when
two periods start on the same date. Always list invoices rather than
constructing names. -o json surfaces the full line-item breakdown for
downstream scripts or spreadsheets.

All amounts are pre-tax; VAT/sales tax is added by Paddle at payment.`,
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
  kupe invoice list -o json | jq '.[] | select(.status.phase=="PastDue")'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := printer.Resolve(f, output)
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
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show one invoice by name",
		Long: `Show a single invoice. NAME is the invoice identifier as it appears in
"kupe invoice list" — usually "{tenant}-{YYYYMMDD}" for the period start
(e.g. "kupe-test-20260301"), but variants exist: a "-final" suffix for a
final invoice issued on cancellation or deletion, and a timestamp suffix
when two periods start on the same date. Always run "kupe invoice list"
first to find the exact name; the format is server-controlled and not
meant to be guessed.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe invoice list                              # find the name
  kupe invoice get kupe-test-20260301
  kupe invoice get kupe-test-20260301 -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
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
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func renderOne(out io.Writer, format *printer.Format, inv *client.Invoice) error {
	return printer.RenderOne(out, format, inv, printer.InvoiceDetailColumns(), func(inv *client.Invoice) string { return inv.Name })
}

func renderList(out io.Writer, format *printer.Format, list []client.Invoice) error {
	return printer.RenderList(out, format, list, printer.InvoiceColumns(), func(inv client.Invoice) string { return inv.Name })
}
