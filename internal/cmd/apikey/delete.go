package apikey

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Revoke an API key by ID",
		Long: `Revoke an API key. Prompts for confirmation on a TTY; use --yes to skip
the prompt (required in non-interactive environments).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe apikey delete 8f2a3e7c-...
  kupe apikey delete 8f2a3e7c-... --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			io := f.IOStreams

			if err := cli.ConfirmDelete(io, yes, "API key", id); err != nil {
				return err
			}

			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteAPIKey(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(io.Out, "apikey/%s revoked\n", id)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
