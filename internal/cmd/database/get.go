package database

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newGetCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show the details of a single database",
		Args:  cobra.ExactArgs(1),
		Example: `  kupe database get orders-db
  kupe db get orders-db -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			db, _, err := api.GetDatabase(cmd.Context(), args[0])
			if err != nil {
				return translateErr(err)
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, db)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
