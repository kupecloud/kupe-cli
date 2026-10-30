package secret

import (
	"fmt"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes, wait bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a secret definition",
		Long: `Delete a secret definition and wait for its removal. Stored values are kept.
The platform withdraws its copies from each namespace. Prompts on TTY;
--yes is required in CI. Pass --wait=false to return once deletion is requested.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams

			if err := cli.ConfirmDelete(io, yes, "secret", name); err != nil {
				return err
			}

			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteSecret(cmd.Context(), name); err != nil {
				return err
			}
			if !wait {
				fmt.Fprintf(io.Out, "secret/%s delete requested\n", name)
				return nil
			}
			if err := cmdutil.WaitForGone(cmd.Context(), io, "secret", name, secretGetter(api, name), timeout); err != nil {
				return secretWaitErr(err, name, true)
			}
			fmt.Fprintf(io.Out, "secret/%s deleted\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", true, "Wait until the definition and the platform's copies are removed")
	cmd.Flags().DurationVar(&timeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
