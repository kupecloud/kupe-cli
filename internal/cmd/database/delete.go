package database

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// deleteNotice is the platform's wording for what deleting a database
// means; help, docs and the confirmation warning repeat it verbatim.
const deleteNotice = "Deleting a database removes it immediately. Its storage is kept for 7 days for support-assisted recovery, then permanently deleted. There is no self-service restore in v1."

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a database and its data",
		Long: `Delete a database. On a TTY you must type the database name to
confirm; pass --yes to skip (required in non-interactive environments).

` + deleteNotice + `

A database with deletion protection on is refused; turn it off first with
"kupe database update NAME --deletion-protection=false".

The request is asynchronous: the database enters the Terminating phase while
its bindings are removed. Follow it with
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
Deleting database %q also removes its Service and credentials Secrets from
every bound cluster namespace.

%s

`, name, deleteNotice)
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
