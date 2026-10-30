package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// backupNoun labels a backup in progress output and confirmations.
const backupNoun = "database backup"

func newBackupCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Take, list and delete manual backups",
		Long: `Manage manual database backups. A manual backup is kept until you delete it,
even after the database is deleted, and a new database can be restored from
it with "kupe database create NAME --from-backup BACKUP". Manual backups are
deleted with your account. A database can keep up to 20.

Automatic backups need no command: they run every day and are set with
--backup-retention and --backup-schedule on "kupe database create" and
"kupe database update".`,
	}

	cmd.AddCommand(newBackupCreateCmd(f))
	cmd.AddCommand(newBackupListCmd(f))
	cmd.AddCommand(newBackupGetCmd(f))
	cmd.AddCommand(newBackupDeleteCmd(f))
	return cmd
}

type backupCreateOpts struct {
	name        string
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newBackupCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &backupCreateOpts{}

	cmd := &cobra.Command{
		Use:   "create DATABASE [--name NAME]",
		Short: "Take a manual backup of a database",
		Long: `Take a manual backup of a running database. Without --name, the backup is
named after the database and the time, in UTC: DATABASE-20261030-141500.

By default the command waits until the backup is Completed, when a database
can be restored from it. It exits at once if the backup fails, with the
platform's reason. Pass --wait=false to return as soon as the request is
accepted, then follow it with "kupe database backup get BACKUP".`,
		Args: cobra.ExactArgs(1),
		Example: `  # Back up before a schema migration
  kupe database backup create orders-db --name orders-db-before-migration

  # Named after the time, without waiting
  kupe db backup create orders-db --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database := args[0]
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, err := api.CreateDatabaseBackup(cmd.Context(), database, client.CreateBackupRequest{Name: opts.name})
			if err != nil {
				return translateBackupCreateErr(err, database)
			}
			if !opts.wait {
				return printer.Backup.One(f.IOStreams, format, b)
			}
			final, werr := cmdutil.WaitForPhaseDone(cmd.Context(), f.IOStreams, backupNoun, b.Name, client.PhaseCompleted,
				"completed", backupPhaseGetter(api, b.Name), opts.waitTimeout)
			if werr != nil {
				return backupWaitErr(werr, b.Name, database, final)
			}
			return printer.Backup.One(f.IOStreams, format, final)
		},
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "Backup name (default DATABASE-YYYYMMDD-HHMMSS, the time in UTC)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the backup to complete before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", time.Hour, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func backupPhaseGetter(api client.Interface, name string) cmdutil.PhaseGetter[*client.Backup] {
	return func(ctx context.Context) (*client.Backup, string, error) {
		b, err := api.GetBackup(ctx, name)
		if err != nil {
			return nil, "", translateBackupErr(err)
		}
		phase := ""
		if b.Status != nil {
			phase = b.Status.Phase
		}
		// A backup being deleted never completes: end the wait now rather
		// than at the timeout.
		if phase == client.PhaseDeleting {
			return nil, "", &cmdutil.TerminalPhaseError{Noun: backupNoun, Name: name, Phase: phase}
		}
		return b, phase, nil
	}
}

// backupWaitErr explains why waiting for backup name of database ended
// before it was Completed. last is the backup as last seen, nil if never
// seen. The backup was requested before the wait in every case.
func backupWaitErr(err error, name, database string, last *client.Backup) error {
	check := "check status:  kupe database backup get " + name
	var terminal *cmdutil.TerminalPhaseError
	switch {
	case errors.As(err, &terminal) && terminal.Phase == client.PhaseDeleting:
		return cli.New(cli.ExitGeneral, fmt.Sprintf("backup %q is being deleted", name)).
			WithHint("it can no longer be restored from; to take another:  kupe database backup create " + database)
	case errors.As(err, &terminal):
		msg := fmt.Sprintf("backup %q failed", name)
		if reason := last.Status.Message(); reason != "" {
			msg += ": " + reason
		}
		return cli.New(cli.ExitGeneral, msg).WithHint(
			"a failed backup cannot be restored from; delete it and take another:  kupe database backup delete " + name)
	case errors.Is(err, ux.ErrWaitTimeout):
		msg := fmt.Sprintf("backup %q is not Completed yet", name)
		if last != nil && last.Status != nil && last.Status.Phase != "" {
			msg = fmt.Sprintf("backup %q is still %s", name, last.Status.Phase)
		}
		return cli.TimeoutError(msg).WithHint("the backup was requested and the platform is still taking it\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting; backup %q was requested and is still being taken", name)).
			WithHint(check)
	case cmdutil.IsTransientWaitErr(err):
		return cli.Wrap(cli.ExitUnavailable, "lost contact with kupe-api while waiting", err).WithHint(check)
	}
	return err
}

func newBackupListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list [DATABASE]",
		Short: "List manual backups",
		Long: `List manual backups, newest first. Without DATABASE, every backup of the
tenant; with it, the backups taken of databases with that name. Both include
the backups of deleted databases, marked "(deleted)" after the database name.`,
		Args: cobra.MaximumNArgs(1),
		Example: `  kupe database backup list
  kupe db backup list orders-db -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			database := ""
			if len(args) == 1 {
				database = args[0]
			}
			list, err := api.ListBackups(cmd.Context(), database)
			if err != nil {
				return translateBackupErr(err)
			}
			return printer.Backup.List(f.IOStreams, format, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newBackupGetCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "get BACKUP",
		Short: "Show a manual backup",
		Long: `Show a manual backup: the database it was taken of, its phase, size and
when it completed, and the version and storage the database had. A database
restored from the backup runs that version and needs at least that storage.`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe database backup get orders-db-before-migration`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, err := api.GetBackup(cmd.Context(), args[0])
			if err != nil {
				return translateBackupErr(err)
			}
			return printer.Backup.One(f.IOStreams, format, b)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newBackupDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete BACKUP",
		Short: "Delete a manual backup",
		Long: `Delete a manual backup. It can no longer be restored from. This works while
the platform has databases or backups switched off, so backups can always be
deleted.

The request is asynchronous. Prompts on a TTY; pass --yes to skip (required
in non-interactive environments).`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe database backup delete orders-db-before-migration --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams
			if err := cli.ConfirmDelete(io, yes, backupNoun, name); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteBackup(cmd.Context(), name); err != nil {
				return translateBackupErr(err)
			}
			fmt.Fprintf(io.Out, "backup/%s delete requested\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
