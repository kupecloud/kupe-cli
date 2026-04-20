// Package plan wires the "kupe plan" subcommand tree — list + get against
// the public /api/v1/plans endpoints. Useful during onboarding / upgrade
// flows and for docs/CI that verify catalog state.
package plan

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// NewCmd returns the parent plan command.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Browse the platform plan catalog",
		Long: `Read-only access to the platform plans available to tenants: resource
pool, observability pool, platform fee, and per-plan limits.

The server endpoint is public, so plan list / plan get work without a
logged-in context. When a context is configured the CLI still honours
its --api-url for routing; tenant and token are not consulted.`,
	}
	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newGetCmd(f))
	return cmd
}

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List platform plans",
		Example: `  kupe plan list`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.PublicClient()
			if err != nil {
				return err
			}
			list, err := api.ListPlans(cmd.Context())
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
		Use:     "get NAME",
		Short:   "Show one plan (e.g. starter, pro, business)",
		Args:    cobra.ExactArgs(1),
		Example: `  kupe plan get pro`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.PublicClient()
			if err != nil {
				return err
			}
			p, err := api.GetPlan(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, format, p)
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

func renderOne(out io.Writer, format *printer.Format, p *client.Plan) error {
	return printer.RenderOne(out, format, p, printer.PlanDetailColumns(), func(p *client.Plan) string { return p.Name })
}

func renderList(out io.Writer, format *printer.Format, list []client.Plan) error {
	return printer.RenderList(out, format, list, printer.PlanColumns(), func(p client.Plan) string { return p.Name })
}
