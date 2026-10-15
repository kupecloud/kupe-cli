// Package database wires the "kupe database" (alias "db") subcommand tree
// for managed PostgreSQL databases and their bindings into cluster
// namespaces. Credentials are never shown: applications receive them
// through bindings.
package database

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// noun is the command noun used in progress labels, hints and
// "database/NAME ..." output lines.
const noun = "database"

// NewCmd returns the parent database command with every subcommand wired in.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "database",
		Aliases: []string{"db"},
		Short:   "Manage PostgreSQL databases",
		Long: `Create, inspect, resize, upgrade, and delete PostgreSQL databases,
and bind them into namespaces of your clusters.

A database belongs to the tenant, not to a cluster: deleting a cluster never
deletes a database. Applications reach a database through a binding, which
delivers a Service named after the database and a credentials Secret into one
namespace of one cluster. Passwords are never shown by the CLI.

Resources (--cpu, --memory, --storage) are per instance and are billed at the
standard unit rates for every hour the database exists.`,
	}

	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newGetCmd(f))
	cmd.AddCommand(newCreateCmd(f))
	cmd.AddCommand(newUpdateCmd(f))
	cmd.AddCommand(newUpgradeCmd(f))
	cmd.AddCommand(newDeleteCmd(f))
	cmd.AddCommand(newRotateCredentialsCmd(f))
	cmd.AddCommand(newWaitCmd(f))
	cmd.AddCommand(newBindingCmd(f))
	return cmd
}

func renderOne(out io.Writer, colorEnabled bool, format *printer.Format, db *client.Database) error {
	return printer.RenderOne(out, format, db, printer.DatabaseDetailColumns(colorEnabled), func(db *client.Database) string { return db.Name })
}

func renderList(out io.Writer, colorEnabled bool, format *printer.Format, dbs []client.Database) error {
	return printer.RenderList(out, format, dbs, printer.DatabaseColumns(colorEnabled), func(db client.Database) string { return db.Name })
}

// phaseGetter adapts GetDatabase to the shared wait helpers.
func phaseGetter(api client.Interface, name string) cmdutil.PhaseGetter[*client.Database] {
	return func(ctx context.Context) (*client.Database, string, error) {
		db, _, err := api.Databases().Get(ctx, name)
		if err != nil {
			return nil, "", translateErr(err)
		}
		if db.Status == nil {
			return db, "", nil
		}
		return db, db.Status.Phase, nil
	}
}

// validateRole rejects anything but the two database roles before a
// request is made.
func validateRole(role string) error {
	switch role {
	case client.DatabaseRoleOwner, client.DatabaseRoleApp:
		return nil
	case "":
		return cli.MisuseError("--role is required").WithHint("use --role owner (schema changes) or --role app (read and write data)")
	}
	return cli.MisuseError("invalid --role " + role).WithHint("use --role owner (schema changes) or --role app (read and write data)")
}
