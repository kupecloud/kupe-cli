package database

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a database and its data",
		Long: `Delete a managed database. On a TTY you must type the database name to
confirm; pass --yes to skip (required in non-interactive environments).

A database with deletion protection on is refused; turn it off first with
"kupe database update NAME --deletion-protection=false".

Deletion is asynchronous: the database enters the Terminating phase and is
removed in the background. Its storage is kept for 7 days, then permanently
deleted. Follow it with
"kupe database wait NAME --for deleted".`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database delete orders-db
  kupe db delete orders-db --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams

			api, err := f.Client()
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintf(io.ErrOut, `
Deleting database %q will:
  - Remove the database; its storage is kept for 7 days, then permanently deleted.
  - Remove its Service and credentials Secrets from every bound cluster namespace.

`, name)
			}
			if err := cli.ConfirmDeleteByName(io, yes, "database", name); err != nil {
				return err
			}
			if err := api.DeleteDatabase(cmd.Context(), name); err != nil {
				return translateDeleteErr(err, name)
			}
			fmt.Fprintf(io.Out, "database/%s delete requested\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
