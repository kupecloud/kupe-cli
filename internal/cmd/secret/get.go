package secret

import (
	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newGetCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show details of a single managed secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := parsedFormat(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			s, _, err := api.GetSecret(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, s)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|json|yaml|go-template=...")
	return cmd
}
