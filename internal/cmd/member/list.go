package member

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List members of the current tenant",
		Example: `  kupe member list
  kupe member list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListMembers(cmd.Context())
			if err != nil {
				return err
			}
			return renderList(f.IOStreams.Out, format, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|json|yaml|name|go-template=...")
	return cmd
}
