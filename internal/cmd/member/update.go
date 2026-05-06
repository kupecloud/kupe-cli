package member

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	role   string
	output string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:     "update EMAIL",
		Short:   "Change a member's role",
		Args:    cobra.ExactArgs(1),
		Example: `  kupe member update alice@acme.com --role admin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			if !validRole(opts.role) {
				return cli.MisuseError(fmt.Sprintf("invalid --role %q (want admin or readonly)", opts.role))
			}

			api, err := f.Client()
			if err != nil {
				return err
			}
			m, err := api.UpdateMember(cmd.Context(), email, client.UpdateMemberRequest{Role: opts.role})
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, format, m)
		},
	}

	cmd.Flags().StringVar(&opts.role, "role", "", "New role: admin or readonly (required)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	_ = cmd.MarkFlagRequired("role")
	return cmd
}
