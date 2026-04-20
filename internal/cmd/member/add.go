package member

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

type addOpts struct {
	role   string
	output string
}

func newAddCmd(f *cli.Factory) *cobra.Command {
	opts := &addOpts{role: client.RoleReadonly}

	cmd := &cobra.Command{
		Use:   "add EMAIL",
		Short: "Add a user to the current tenant",
		Args:  cobra.ExactArgs(1),
		Example: `  kupe member add alice@acme.com --role admin
  kupe member add bob@acme.com                         # defaults to readonly`,
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]
			format, err := parsedFormat(f, opts.output)
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
			m, err := api.AddMember(cmd.Context(), client.AddMemberRequest{Email: email, Role: opts.role})
			if err != nil {
				return err
			}
			return renderOne(f.IOStreams.Out, format, m)
		},
	}

	cmd.Flags().StringVar(&opts.role, "role", client.RoleReadonly, "Role: admin or readonly")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: table|json|yaml|go-template=...")
	return cmd
}
