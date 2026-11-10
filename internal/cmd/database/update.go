package database

import (
	"fmt"
	"reflect"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	addLogicalDatabases, addSchemas                        []string
	addRoles, removeRoles, addExtensions, removeExtensions []string
	cpu                                                    string
	memory                                                 string
	storage                                                string
	highAvailability                                       bool
	deletionProtection                                     bool
	pooler                                                 poolerFlags
	backupRetention                                        int32
	backupSchedule                                         string
	wait                                                   bool
	waitTimeout                                            time.Duration
	output                                                 string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{wait: true, waitTimeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Change database settings or append logical databases and schemas",
		Long: `Change a database's per-instance resources, high availability, deletion
protection, backup settings, declared roles or PostgreSQL extensions. Only
the flags you pass are changed.

--add-logical-database appends a PostgreSQL database. --add-schema
[DATABASE.]SCHEMA adds a schema, to the first database when DATABASE is
omitted. Repeat either flag for more. Existing entries and schemas are
kept; they cannot be removed. After a restore, append with these flags.

--pooler turns the connection pooler on, and --pooler=false off; --pool-mode
and --pooler-instances change it. Turning it on or off gives readwrite and
readonly bindings a new host in each of their namespaces: applications pick
it up when they re-read the Secret or restart.

` + poolerLongHelp + `

Changing CPU or memory rolls the instances (a highly available database
switches over first). Storage grows online and can never shrink. The major
version cannot be changed yet. Shortening --backup-retention deletes the
backups that fall outside the new window.

Concurrent updates from another client are detected and retried once
automatically. By default the command waits until the change is in effect:
the platform has applied it and every instance runs it again. A database
with a single instance is briefly Degraded while that instance restarts for
a CPU or memory change; the wait rides that out, but fails if the database
stays Degraded for 5 minutes. A change to deletion protection alone is in
effect at once and is not waited on. Pass --wait=false to return as soon as
the change is accepted.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database update orders-db --cpu 1 --memory 2Gi
  kupe db update orders-db --storage 20Gi
  kupe db update orders-db --ha
  kupe db update orders-db --pooler --pool-mode session
  kupe db update orders-db --pooler=false
  kupe db update orders-db --deletion-protection=false
  kupe db update orders-db --backup-retention 14`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			patch := client.PatchDatabaseRequest{}
			if opts.cpu != "" || opts.memory != "" || opts.storage != "" {
				if err := validateResources(opts.cpu, opts.memory, opts.storage, false, false); err != nil {
					return err
				}
				patch.Resources = &client.DatabaseResources{CPU: opts.cpu, Memory: opts.memory, Storage: opts.storage}
			}
			if cmd.Flags().Changed("ha") {
				patch.HighAvailability = &opts.highAvailability
			}
			if cmd.Flags().Changed("deletion-protection") {
				patch.DeletionProtection = &opts.deletionProtection
			}
			backup, err := backupFromFlags(cmd.Flags(), opts.backupRetention, opts.backupSchedule)
			if err != nil {
				return err
			}
			patch.Backup = backup
			pooler, err := poolerFromFlags(cmd.Flags(), opts.pooler, false)
			if err != nil {
				return err
			}
			listChanged := len(opts.addRoles)+len(opts.removeRoles)+len(opts.addExtensions)+len(opts.removeExtensions) > 0
			if err := validateListEdits(opts); err != nil {
				return err
			}
			logicalChanged := logicalRequested(opts.addLogicalDatabases, opts.addSchemas)
			if patch.IsEmpty() && !listChanged && !logicalChanged && pooler == nil {
				return cli.MisuseError("nothing to update: pass at least one of --cpu, --memory, --storage, --ha, --deletion-protection, --backup-retention, --backup-schedule, --pooler, --pool-mode, --pooler-instances, --add-role, --remove-role, --add-extension, --remove-extension, --add-logical-database, --add-schema")
			}

			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			var mutateErr error
			var notes []string
			updated, err := api.Databases().UpdateRMW(cmd.Context(), name, func(current *client.Database) *client.PatchDatabaseRequest {
				patch.Postgres = withPooler(editedPostgres(current.Postgres, opts), pooler)
				if logicalChanged {
					if current.Postgres == nil || current.Postgres.Databases == nil || len(*current.Postgres.Databases) == 0 {
						mutateErr = logicalNotAvailable()
						return nil
					}
					var list []client.LogicalDatabase
					list, notes, mutateErr = appendLogical(*current.Postgres.Databases, opts.addLogicalDatabases, opts.addSchemas)
					if mutateErr != nil {
						return nil
					}
					if !reflect.DeepEqual(list, *current.Postgres.Databases) {
						if patch.Postgres == nil {
							patch.Postgres = &client.DatabasePostgres{}
						}
						patch.Postgres.Databases = &list
					}
				}
				if patch.IsEmpty() {
					return nil
				}
				return &patch
			})
			if err != nil {
				return logicalFeatureErr(translateUpdateErr(err, name), logicalChanged)
			}
			if mutateErr != nil {
				return mutateErr
			}
			for _, note := range notes {
				fmt.Fprintln(f.IOStreams.ErrOut, note)
			}
			if cmd.Flags().Changed(flagPooler) {
				fmt.Fprintf(f.IOStreams.ErrOut,
					"Readwrite and readonly bindings of %q get a new host in each of their namespaces. Applications pick it up when they re-read the Secret or restart.\n", name)
			}
			// Deletion protection is read from the spec when a delete comes
			// in, so it is in effect once the PATCH lands; every other change
			// is applied by the platform first (NeedsWait). Waiting on a
			// protection-only change would hold the "turn protection off,
			// then delete" path on a database that is Degraded or was never
			// provisioned.
			if !opts.wait || !patch.NeedsWait() {
				return printer.Database.One(f.IOStreams, format, updated)
			}
			// The PATCH response carries the generation the change produced.
			wait := service.WaitForConverged
			if logicalChanged {
				wait = waitForLogicalConverged
			}
			final, werr := wait(cmd.Context(), f.IOStreams, api, name, updated.Generation, opts.waitTimeout)
			if werr != nil {
				return postgresWaitErr(cmdutil.MapWaitErr(werr, noun, name, "update"), final)
			}
			if final == nil {
				final = updated
			}
			return printer.Database.One(f.IOStreams, format, final)
		},
	}

	cmd.Flags().StringArrayVar(&opts.addLogicalDatabases, "add-logical-database", nil, "Logical database to append; repeat for each; existing entries cannot be removed")
	cmd.Flags().StringArrayVar(&opts.addSchemas, "add-schema", nil, "Schema to append as [DATABASE.]SCHEMA; default the first database; repeat for each")
	cmd.Flags().StringArrayVar(&opts.addRoles, "add-role", nil, "Declared role to add; repeat for each role")
	cmd.Flags().StringArrayVar(&opts.removeRoles, "remove-role", nil, "Declared role to remove; repeat for each role")
	cmd.Flags().StringArrayVar(&opts.addExtensions, "add-extension", nil, "Extension to add; repeat for each extension")
	cmd.Flags().StringArrayVar(&opts.removeExtensions, "remove-extension", nil, "Extension to remove; repeat for each extension")
	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "New CPU per instance (e.g. 1, 500m)")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "New memory per instance (e.g. 2Gi)")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "New storage per instance (e.g. 20Gi); grow only")
	cmd.Flags().BoolVar(&opts.highAvailability, "ha", false, "Turn high availability on (--ha) or off (--ha=false)")
	cmd.Flags().BoolVar(&opts.deletionProtection, "deletion-protection", false, "Turn deletion protection on (--deletion-protection) or off (--deletion-protection=false)")
	cmd.Flags().Int32Var(&opts.backupRetention, flagBackupRetention, 0, "New backup retention in `days` (the platform sets the range); shortening it deletes the backups that fall outside the new window")
	cmd.Flags().StringVar(&opts.backupSchedule, flagBackupSchedule, "", "New backup schedule: Daily or DailyThenWeekly")
	addPoolerFlags(cmd, &opts.pooler, "Turn the connection pooler on (--pooler) or off (--pooler=false)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the change is in effect before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
