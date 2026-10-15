package database

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List databases in the tenant",
		Example: `  kupe database list
  kupe db list -o wide
  kupe db list -o name`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.Databases().List(cmd.Context())
			if err != nil {
				return translateErr(err, "")
			}
			return printer.Database.List(f.IOStreams, format, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}
