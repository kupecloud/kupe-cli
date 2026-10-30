package database

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type createOpts struct {
	engine               string
	version              string
	cpu                  string
	memory               string
	storage              string
	highAvailability     bool
	noDeletionProtection bool
	backupRetention      int32
	backupSchedule       string
	fromBackup           string
	from                 string
	pointInTime          string
	wait                 bool
	waitTimeout          time.Duration
	output               string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a database",
		Long: `Create a PostgreSQL database and (by default) wait for it to reach
the Running phase. Pass --wait=false to return as soon as the request is
accepted, then use "kupe database wait NAME".

--cpu, --memory and --storage are required (a restore takes the source's
storage by default) and are per instance: a highly available database (--ha)
runs two instances, each reserving and billing for these resources. Storage
can grow later but never shrink.

Deletion protection is on by default; turn it off with
"kupe database update NAME --deletion-protection=false" before deleting, or
create the database with --no-deletion-protection.

Backups are automatic. --backup-retention sets how far back they reach, in
days, and --backup-schedule how many full backups are kept within that time;
leave them out to take the defaults, 7 days and Daily. While the platform
does not offer backups, either flag is refused.

--from-backup restores the new database from a manual backup instead of
starting it empty. --from restores it from another database as it was at
--point-in-time, a moment inside that database's restore window (RFC 3339,
for example 2026-10-30T09:00:00Z); without --point-in-time, the latest point.
The new database runs the source's major version: leave out --version, or
give that one. Its storage must be at least the source's; --storage defaults
to the source's. The source is unchanged, and the new database counts against
your plan's database limit like any other.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Smallest database, wait for Running
  kupe database create orders-db --cpu 250m --memory 512Mi --storage 5Gi

  # Highly available, specific major version, fire-and-forget
  kupe db create orders-db --version 17 --cpu 1 --memory 2Gi --storage 20Gi --ha --wait=false

  # 14 days of backups: a full backup for each of the last 7 days, one a week before that
  kupe db create orders-db --cpu 500m --memory 1Gi --storage 10Gi --backup-retention 14 --backup-schedule DailyThenWeekly

  # Restore a manual backup into a new database, with the backup's storage
  kupe db create orders-db-copy --from-backup orders-db-before-migration --cpu 500m --memory 1Gi

  # Restore orders-db as it was at 09:00 UTC
  kupe db create orders-db-0900 --from orders-db --point-in-time 2026-10-30T09:00:00Z --cpu 500m --memory 1Gi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateCreateOpts(opts); err != nil {
				return err
			}
			source := restoreSource(opts)
			backup, err := backupFromFlags(cmd.Flags(), opts.backupRetention, opts.backupSchedule)
			if err != nil {
				return err
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if source != nil && opts.storage == "" {
				if opts.storage, err = sourceStorage(cmd.Context(), api, source); err != nil {
					return err
				}
				// Stderr so -o json/yaml output stays clean.
				fmt.Fprintf(f.IOStreams.ErrOut, "Using the source's storage, %s; pass --storage to give the new database more.\n", opts.storage)
			}

			req := client.CreateDatabaseRequest{
				Name:             name,
				Engine:           opts.engine,
				Version:          opts.version,
				Resources:        &client.DatabaseResources{CPU: opts.cpu, Memory: opts.memory, Storage: opts.storage},
				HighAvailability: opts.highAvailability,
				Backup:           backup,
				Source:           source,
			}
			if opts.noDeletionProtection {
				off := false
				req.DeletionProtection = &off
			}
			if opts.highAvailability {
				// Stderr so -o json/yaml output stays clean.
				fmt.Fprintf(f.IOStreams.ErrOut,
					"Creating %q with high availability: two instances, each reserving and billing for the requested CPU, memory and storage.\n\n", name)
			}

			created, _, err := api.Databases().Create(cmd.Context(), req)
			if err != nil {
				return translateCreateErr(err, name, source)
			}
			if !opts.wait {
				return printer.Database.One(f.IOStreams, format, created)
			}

			final, werr := service.WaitForPhase(cmd.Context(), f.IOStreams, api, name, client.PhaseRunning, opts.waitTimeout)
			if werr != nil {
				return cmdutil.MapWaitErr(werr, noun, name, "create")
			}
			if final == nil {
				final = created
			}
			return printer.Database.One(f.IOStreams, format, final)
		},
	}

	cmd.Flags().StringVar(&opts.engine, "engine", "postgres", "Database engine (only postgres is supported)")
	cmd.Flags().StringVar(&opts.version, "version", "", "Engine major version (e.g. 18). Defaults to the platform default if unset, or with --from-backup or --from to the source's.")
	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "CPU per instance, without a unit (e.g. 500m, 2) (required)")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "Memory per instance, with a unit (e.g. 1Gi, 512Mi) (required)")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "Storage per instance, with a unit (e.g. 10Gi); can grow later, never shrink (required, except with --from-backup or --from: default the source's)")
	cmd.Flags().BoolVar(&opts.highAvailability, "ha", false, "Run two instances with a synchronous standby; resources are reserved and billed per instance")
	cmd.Flags().BoolVar(&opts.noDeletionProtection, "no-deletion-protection", false, "Create without deletion protection (it is on by default)")
	cmd.Flags().Int32Var(&opts.backupRetention, flagBackupRetention, 0, "How far back backups reach, in `days`; the platform sets the range (default 7)")
	cmd.Flags().StringVar(&opts.backupSchedule, flagBackupSchedule, "", "Full backups kept within the retention: Daily (one for every day, the default) or DailyThenWeekly (one for each of the last 7 days, then one a week)")
	cmd.Flags().StringVar(&opts.fromBackup, flagFromBackup, "", "Restore the new database from this manual backup")
	cmd.Flags().StringVar(&opts.from, flagFrom, "", "Restore the new database from this database, as it was at --point-in-time")
	cmd.Flags().StringVar(&opts.pointInTime, flagPointInTime, "", "Moment to restore --from to, inside its restore window, in RFC 3339 (e.g. 2026-10-30T09:00:00Z); default the latest point")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the database to reach Running before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// Restore flags of create.
const (
	flagFromBackup  = "from-backup"
	flagFrom        = "from"
	flagPointInTime = "point-in-time"
)

// validateCreateOpts catches missing or malformed resources and restore
// flags before the request; the server remains authoritative for floors
// and ceilings and for whether the source exists and fits. With a source,
// storage may be left out: it defaults to the source's.
func validateCreateOpts(opts *createOpts) error {
	if err := validateRestoreOpts(opts); err != nil {
		return err
	}
	restoring := opts.fromBackup != "" || opts.from != ""
	return validateResources(opts.cpu, opts.memory, opts.storage, true, !restoring)
}

// validateRestoreOpts allows one source, and --point-in-time only with
// --from, in RFC 3339.
func validateRestoreOpts(opts *createOpts) error {
	switch {
	case opts.fromBackup != "" && opts.from != "":
		return cli.MisuseError("--from-backup and --from cannot be used together").
			WithHint("restore from a manual backup with --from-backup, or from a database with --from")
	case opts.pointInTime != "" && opts.from == "":
		return cli.MisuseError("--point-in-time needs --from").
			WithHint("a backup restores to the moment it completed; --point-in-time picks a moment of the database given with --from")
	case opts.pointInTime != "":
		if _, err := time.Parse(time.RFC3339, opts.pointInTime); err != nil {
			return cli.MisuseError("invalid --point-in-time " + opts.pointInTime).
				WithHint("use RFC 3339, for example 2026-10-30T09:00:00Z")
		}
	}
	return nil
}

// restoreSource is the request's source from the restore flags, nil
// without one.
func restoreSource(opts *createOpts) *client.DatabaseSource {
	if opts.fromBackup == "" && opts.from == "" {
		return nil
	}
	return &client.DatabaseSource{Backup: opts.fromBackup, Database: opts.from, PointInTime: opts.pointInTime}
}

// sourceStorage is the storage a restore of source needs at least: the
// backup's database's when it was taken, or the source database's.
func sourceStorage(ctx context.Context, api client.Interface, source *client.DatabaseSource) (string, error) {
	if source.Backup != "" {
		b, err := api.GetBackup(ctx, source.Backup)
		if err != nil {
			return "", translateBackupErr(err)
		}
		if b.Status == nil || b.Status.Source == nil || b.Status.Source.Storage == "" {
			return "", cli.ConflictError(fmt.Sprintf("backup %q has not recorded its storage yet", source.Backup)).
				WithHint("restore once it is Completed:  kupe database backup get " + source.Backup + "\nor pass --storage")
		}
		return b.Status.Source.Storage, nil
	}
	db, _, err := api.Databases().Get(ctx, source.Database)
	if err != nil {
		return "", translateErr(err, source.Database)
	}
	if db.Resources == nil || db.Resources.Storage == "" {
		return "", cli.MisuseError(fmt.Sprintf("database %q reports no storage", source.Database)).
			WithHint("pass --storage")
	}
	return db.Resources.Storage, nil
}

// maxQuantityLength is the longest CPU, memory or storage value the
// database API accepts.
const maxQuantityLength = 20

// validateResources checks the per-instance resource flags: CPU and memory
// are required on create and optional on update; storage is required on a
// create that does not restore (storageRequired).
func validateResources(cpu, memory, storage string, required, storageRequired bool) error {
	return cmdutil.ValidateQuantities(maxQuantityLength,
		cmdutil.QuantityFlag{Name: "--cpu", Value: cpu, CPU: true, Required: required, Example: "500m"},
		cmdutil.QuantityFlag{Name: "--memory", Value: memory, Required: required, Example: "1Gi"},
		cmdutil.QuantityFlag{Name: "--storage", Value: storage, Required: storageRequired, Example: "10Gi"},
	)
}
