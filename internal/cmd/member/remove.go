package member

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newRemoveCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "remove EMAIL",
		Short: "Remove a member from the tenant",
		Long: `Remove a user from the tenant. Prompts on TTY; --yes required in CI.
The user loses Authentik group membership on the next operator reconcile,
which revokes console and kubeconfig access.`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe member remove ex-employee@acme.com --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]
			io := f.IOStreams

			if err := cli.ConfirmDelete(io, yes, "tenant member", email); err != nil {
				return err
			}

			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.RemoveMember(cmd.Context(), email); err != nil {
				return err
			}
			fmt.Fprintf(io.Out, "member/%s removed\n", email)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
