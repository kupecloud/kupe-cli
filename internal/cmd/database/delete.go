package database

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

// deleteNotice is the platform's wording for what deleting a database
// means; help, docs and the confirmation warning repeat it verbatim.
const deleteNotice = "Deleting a database removes it immediately. Its storage is kept for 7 days for support-assisted recovery, then permanently deleted. There is no self-service restore in v1."

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	return service.DeleteCmd(f, cmdutil.Delete{
		Help: cmdutil.Help{
			Short: "Delete a database and its data",
			Long: `Delete a database. On a TTY you must type the database name to
confirm; pass --yes to skip (required in non-interactive environments).

` + deleteNotice + `

A database with deletion protection on is refused; turn it off first with
"kupe database update NAME --deletion-protection=false".

By default the command waits until the database is gone. It is Terminating
while its bindings are removed from every cluster namespace and its instances
stop, which can take several minutes. Pass --wait=false to return as soon as
the request is accepted, then follow it with
"kupe database wait NAME --for deleted".`,
			Example: `  kupe database delete orders-db
  kupe db delete orders-db --yes --wait=false`,
		},
		Warning:       printDeleteDatabaseWarning,
		ConfirmByName: true,
		Translate:     translateDeleteErr,
		WaitTimeout:   15 * time.Minute,
	})
}

// printDeleteDatabaseWarning writes what deleting a database removes to
// stderr, before the prompt.
func printDeleteDatabaseWarning(io *cli.IOStreams, name string) {
	fmt.Fprintf(io.ErrOut, `
Deleting database %q also removes its Service and credentials Secrets from
every bound cluster namespace.

%s

`, name, deleteNotice)
}
