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
Console and API access ends within a minute. Kubeconfigs issued before the
removal keep working until their token expires: up to 8 hours for a
signed-in kubeconfig, and the lifetime chosen at download for a token
kubeconfig.`,
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
