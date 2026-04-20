package secret

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed secrets in the tenant",
		Example: `  kupe secret list
  kupe secret list -o wide
  kupe secret list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListSecrets(cmd.Context())
			if err != nil {
				return err
			}
			return renderList(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|wide|json|yaml|name|go-template=...")
	return cmd
}
