// Package database wires the "kupe database" (alias "db") subcommand tree
// for managed PostgreSQL databases and their bindings into cluster
// namespaces. Credentials are never shown: applications receive them
// through bindings.
package database

import (
	"slices"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
		Long: `Create, inspect, resize, and delete PostgreSQL databases,
and bind them into namespaces of your clusters.

A database belongs to the tenant, not to a cluster: deleting a cluster never
deletes a database. Applications reach a database through a binding, which
delivers a Service named after the database and a credentials Secret into
namespaces of one cluster. Passwords are never shown by the CLI.

Resources (--cpu, --memory, --storage) are per instance and are billed at the
standard unit rates: CPU and memory for every hour an instance is running,
storage for every hour its volume is bound.`,
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

// service is the database's shared command surface: list, get, wait and
// delete are built from it, and create, update and rotate-credentials wait
// and translate through it.
var service = cmdutil.Service[client.Database, client.CreateDatabaseRequest, client.PatchDatabaseRequest]{
	Noun: noun,
	API:  client.Interface.Databases,
	Phase: func(db *client.Database) string {
		if db.Status == nil {
			return ""
		}
		return db.Status.Phase
	},
	// A change has taken effect once the operator has applied the spec, the
	// phase is Running and the Ready condition is True, so every instance
	// runs the new spec. A single-instance database is Degraded while its
	// only instance restarts to apply a CPU or memory change, so Degraded
	// fails the wait only once it has lasted degradedGrace.
	Converge: cmdutil.Converge[*client.Database]{
		ObservedGeneration: func(db *client.Database) int64 {
			if db == nil || db.Status == nil {
				return 0
			}
			return db.Status.ObservedGeneration
		},
		Settled:       func(db *client.Database) bool { return db != nil && db.Status.Ready() },
		DegradedGrace: degradedGrace,
	},
	Printer:   printer.Database,
	Translate: translateErr,
}

// degradedGrace bounds how long an update waits through a Degraded phase.
const degradedGrace = 5 * time.Minute

// Backup flags, shared by create and update.
const (
	flagBackupRetention = "backup-retention"
	flagBackupSchedule  = "backup-schedule"
)

// backupFromFlags is the backup settings the user passed, nil when neither
// backup flag was: only a flag that was passed is sent. The schedule must
// be one of client.DatabaseBackupSchedules, exactly as the API spells it.
// The retention range is the platform's, so the API checks it.
func backupFromFlags(flags *pflag.FlagSet, retention int32, schedule string) (*client.DatabaseBackup, error) {
	var b client.DatabaseBackup
	if flags.Changed(flagBackupRetention) {
		b.Retention = &retention
	}
	if flags.Changed(flagBackupSchedule) {
		if !slices.Contains(client.DatabaseBackupSchedules, schedule) {
			return nil, cli.MisuseError("invalid --backup-schedule " + schedule).WithHint(backupScheduleHint)
		}
		b.Schedule = schedule
	}
	if b == (client.DatabaseBackup{}) {
		return nil, nil
	}
	return &b, nil
}

// backupScheduleHint names both schedules.
const backupScheduleHint = "use --backup-schedule Daily or --backup-schedule DailyThenWeekly"

// validateRole rejects anything but the database roles before a request is
// made. Every invalid value gets the same message and hint.
func validateRole(role string) error {
	if slices.Contains(client.DatabaseRoles, role) {
		return nil
	}
	if role == "" {
		return cli.MisuseError("--role is required").WithHint(roleHint)
	}
	return cli.MisuseError("invalid --role " + role).WithHint(roleHint)
}

// roleHint names every role and what it can do.
const roleHint = "use --role owner (schema changes), --role readwrite (read and write data) or --role readonly (read data)"
