package database

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

type rotateOpts struct {
	role string
	yes  bool
}

func newRotateCredentialsCmd(f *cli.Factory) *cobra.Command {
	opts := &rotateOpts{}

	cmd := &cobra.Command{
		Use:   "rotate-credentials NAME --role owner|app",
		Short: "Rotate the password of one database role",
		Long: `Request a new password for one database role: owner (schema changes) or
app (read and write data).

The current password stops working as soon as the rotation completes. Every
bound credentials Secret for that role is updated, but applications using the
old password must restart to pick up the new one. No password is shown.

Rotate after removing a binding if that namespace should also lose knowledge
of the password. Prompts on a TTY; pass --yes to skip (required in
non-interactive environments).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database rotate-credentials orders-db --role app
  kupe db rotate-credentials orders-db --role owner --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams
			if err := validateRole(opts.role); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}

			fmt.Fprintf(io.ErrOut, `
Rotating the %s password of database %q:
  - The current password stops working once the rotation completes.
  - Bound credentials Secrets are updated; applications using the old password must restart.

`, opts.role, name)
			prompt := fmt.Sprintf("This will rotate the %s password of database %q.", opts.role, name)
			if err := cli.ConfirmAction(io, opts.yes, "rotate credentials", prompt); err != nil {
				return err
			}
			if _, err := api.RotateDatabaseCredentials(cmd.Context(), name, opts.role); err != nil {
				return translateRotateErr(err, name)
			}
			fmt.Fprintf(io.Out, "database/%s %s credentials rotation requested\n", name, opts.role)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.role, "role", "", "Role whose password is rotated: owner or app (required)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
