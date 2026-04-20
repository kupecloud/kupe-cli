package secret

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a managed secret",
		Long: `Delete a managed secret. Prompts on TTY; --yes required in CI. Only the
ManagedSecret resource and its mirrored Kubernetes Secrets are removed —
the underlying value in OpenBao is left alone so other consumers can
continue to read it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams

			if err := cli.ConfirmDelete(io, yes, "managed secret", name); err != nil {
				return err
			}

			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteSecret(cmd.Context(), name); err != nil {
				return err
			}
			fmt.Fprintf(io.Out, "secret/%s deleted\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
