package apikey

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List API keys for the current tenant",
		Long: `List every API key for the tenant. The raw key value is never included
in the response — only metadata (ID, name, role, last-used, age).`,
		Example: `  kupe apikey list
  kupe apikey list -o wide
  kupe apikey list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := printer.MustParse(output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			keys, err := api.ListAPIKeys(cmd.Context())
			if err != nil {
				return err
			}
			return renderList(f.IOStreams.Out, format, keys)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|wide|json|yaml|name|go-template=...")
	return cmd
}

func renderList(out io.Writer, format *printer.Format, keys []client.APIKey) error {
	return printer.RenderList(out, format, keys, printer.APIKeyColumns(), func(k client.APIKey) string { return k.ID })
}
