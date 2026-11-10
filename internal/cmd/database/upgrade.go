package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// majorVersion matches an engine major such as "18", the only form the
// API accepts for a version.
var majorVersion = regexp.MustCompile(`^[1-9]\d$`)

type upgradeOpts struct {
	version     string
	output      string
	waitTimeout time.Duration
	cancel      bool
	yes         bool
	wait        bool
}

func newUpgradeCmd(f *cli.Factory) *cobra.Command {
	opts := &upgradeOpts{wait: true, waitTimeout: 60 * time.Minute}

	cmd := &cobra.Command{
		Use:   "upgrade NAME (--version MAJOR | --cancel)",
		Short: "Upgrade a database to the next major version",
		Long: `Move a database to the next PostgreSQL major version in place. Upgrades go
one major at a time. The platform first takes a backup, then stops the
database and moves its data to the new major. The database is offline, and
applications cannot connect to it, until the upgrade finishes. The command
asks you to confirm before it starts; pass --yes to skip the question.

The platform only starts an upgrade when the database is running and
healthy, has a recent backup and has room for one more backup. If one of
those is not true yet, the command exits 5 and the message says which.

By default the command waits until the database runs the new major, up to
--wait-timeout. If the platform stops the upgrade before it changes any
data, the database goes on running its old major. If the upgrade fails, the
message says whether the database still runs its old major. In both cases
the command exits 1, and the platform leaves the database as it is until you
set the version back with --cancel. Fix the cause, then run the upgrade
again. The backup taken before the upgrade is listed by
"kupe database backup list NAME".

--cancel sets the version back to the major the database runs. It cancels an
upgrade that is waiting, taking its backup, rolled back, or failed. It
needs no confirmation and does not wait. Once the database is offline and
moving its data, the platform refuses it.

Requesting the major the database already runs changes nothing.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database upgrade orders-db --version 18
  kupe database upgrade orders-db --version 18 --yes --wait=false
  kupe database upgrade orders-db --cancel`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd.Context(), f, args[0], opts, cmd.Flags().Changed("version"))
		},
	}

	cmd.Flags().StringVar(&opts.version, "version", "", "Target engine major version, one above the running one (e.g. 18); required unless --cancel")
	cmd.Flags().BoolVar(&opts.cancel, "cancel", false, "Set the version back to the running major, cancelling an upgrade that is waiting, taking its backup, rolled back or failed")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the database runs the new major before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 60*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func runUpgrade(ctx context.Context, f *cli.Factory, name string, opts *upgradeOpts, versionSet bool) error {
	switch {
	case opts.cancel && versionSet:
		return cli.MisuseError("--cancel and --version cannot be used together")
	case !opts.cancel && opts.version == "":
		return cli.MisuseError("--version is required, for example --version 18; or pass --cancel")
	case !opts.cancel && !majorVersion.MatchString(opts.version):
		return cli.MisuseError(fmt.Sprintf("--version must be a major version such as 18; got %q", opts.version))
	}
	format, err := printer.Resolve(f, opts.output)
	if err != nil {
		return err
	}
	api, err := f.Client()
	if err != nil {
		return err
	}

	if opts.cancel {
		return cancelUpgrade(ctx, f, api, name, format)
	}

	// Read first so the question names the majors involved, and so a version
	// that is already requested neither prompts nor sends anything.
	current, _, err := api.Databases().Get(ctx, name)
	if err != nil {
		return translateUpgradeErr(err, name)
	}
	updated := current
	if current.Version != opts.version {
		from := runningMajor(current)
		if from == "" {
			from = current.Version
		}
		prompt := fmt.Sprintf("This takes database %s offline while it upgrades from %s to %s.", name, from, opts.version)
		if err := cli.ConfirmAction(f.IOStreams, opts.yes, "upgrade", prompt); err != nil {
			return err
		}
		updated, err = api.Databases().UpdateRMW(ctx, name, func(db *client.Database) *client.PatchDatabaseRequest {
			if db.Version == opts.version {
				return nil
			}
			return &client.PatchDatabaseRequest{Version: &opts.version}
		})
		if err != nil {
			return translateUpgradeErr(err, name)
		}
	}
	// Running the requested major already: nothing to do or wait for. The
	// version is requested but not running yet (an upgrade already asked
	// for): wait for it like a fresh one.
	if !opts.wait || runningMajor(updated) == opts.version {
		return printer.Database.One(f.IOStreams, format, updated)
	}

	done, err := waitForUpgrade(ctx, f.IOStreams, api, name, opts.version, updated.Generation, opts.waitTimeout)
	if err != nil {
		return upgradeWaitErr(err, name)
	}
	if err := printer.Database.One(f.IOStreams, format, done); err != nil {
		return err
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "The platform took a backup before the upgrade. List it with:  kupe database backup list %s\n", name)
	return nil
}

// cancelUpgrade sets the version back to the major the database runs. The
// platform accepts that whatever it says about offering upgrades.
func cancelUpgrade(ctx context.Context, f *cli.Factory, api client.Interface, name string, format *printer.Format) error {
	var refusal error
	updated, err := api.Databases().UpdateRMW(ctx, name, func(db *client.Database) *client.PatchDatabaseRequest {
		refusal = nil
		running := runningMajor(db)
		switch {
		case running == "":
			refusal = cli.MisuseError(fmt.Sprintf("database %q does not report the version it runs yet, so there is nothing to cancel", name)).
				WithHint(fmt.Sprintf("check it:  kupe db get %s", name))
			return nil
		case db.Version == running:
			refusal = cli.MisuseError(fmt.Sprintf("nothing to cancel: database %q is set to the major it runs, %s", name, running))
			return nil
		}
		return &client.PatchDatabaseRequest{Version: &running}
	})
	if err != nil {
		return translateUpgradeErr(err, name)
	}
	if refusal != nil {
		return refusal
	}
	return printer.Database.One(f.IOStreams, format, updated)
}

// runningMajor is the major of the version the database runs, "" until the
// platform reports one.
func runningMajor(db *client.Database) string {
	if db == nil || db.Status == nil {
		return ""
	}
	major, _, _ := strings.Cut(db.Status.CurrentVersion, ".")
	return major
}

// upgradeEnded is why a wait ended with the upgrade not done: the platform
// rolled it back or it failed.
type upgradeEnded struct {
	reason, message string
}

func (e *upgradeEnded) Error() string { return e.message }

// waitForUpgrade polls the database until it runs target. The upgrade is
// done when the phase is Running, the operator has applied the version
// change (observedGeneration has caught up with generation), Ready is True
// and the running major is the target. Until the operator has seen the
// change, nothing in the status counts: a Ready reason left over from an
// earlier attempt must not end this wait. Once it has, Ready False with
// UpgradeRolledBack or UpgradeFailed ends the wait with an *upgradeEnded,
// whatever the phase (a failed upgrade is Degraded). Upgrading, a waiting
// check, the backup and an HA standby rebuilding after the upgrade (Running
// with Ready False) are progress. A database being deleted ends the wait.
func waitForUpgrade(ctx context.Context, io *cli.IOStreams, api client.Interface, name, target string, generation int64, timeout time.Duration) (*client.Database, error) {
	var latest *client.Database
	poll := cmdutil.PollPhase(service.Getter(api, name), func(db *client.Database, phase string) (bool, error) {
		latest = db
		if phase == client.PhaseTerminating {
			return false, &cmdutil.TerminalPhaseError{Noun: noun, Name: name, Phase: phase}
		}
		st := db.Status
		if st == nil || st.ObservedGeneration < max(generation, db.Generation) {
			return false, nil
		}
		if ready := st.Condition("Ready"); ready != nil && ready.Status != "True" &&
			(ready.Reason == client.ReasonUpgradeRolledBack || ready.Reason == client.ReasonUpgradeFailed) {
			return false, &upgradeEnded{reason: ready.Reason, message: ready.Message}
		}
		return phase == client.PhaseRunning && st.Ready() && runningMajor(db) == target, nil
	})
	err := ux.WaitFor(ctx, io, ux.WaitForOpts{
		Label:         fmt.Sprintf("database %s", name),
		DoneVerb:      "upgraded",
		PhaseOverride: "Upgrading",
		Poll:          poll,
		Timeout:       timeout,
	})
	return latest, err
}

// upgradeWaitErr explains why waiting for an upgrade ended before the
// database ran the new major. The upgrade was requested in every case.
func upgradeWaitErr(err error, name string) error {
	check := fmt.Sprintf("check status:  kupe database get %s", name)
	var ended *upgradeEnded
	switch {
	case errors.As(err, &ended):
		msg := ended.message
		if ended.reason == client.ReasonUpgradeRolledBack {
			if msg == "" {
				msg = "the platform stopped the upgrade before it changed any data"
			}
			return cli.New(cli.ExitGeneral, fmt.Sprintf("the upgrade of database %q was rolled back: %s", name, msg)).
				WithHint(fmt.Sprintf("the database still runs its old major\nthe platform leaves it as it is until you set the version back:  kupe db upgrade %s --cancel", name))
		}
		if msg == "" {
			msg = "the platform could not finish the upgrade"
		}
		return cli.New(cli.ExitGeneral, fmt.Sprintf("the upgrade of database %q failed: %s", name, msg)).
			WithHint(fmt.Sprintf("the backup taken before the upgrade:  kupe database backup list %s\nto clear the failed upgrade and try again:  kupe db upgrade %s --cancel", name, name))
	case errors.Is(err, ux.ErrWaitTimeout):
		return cli.TimeoutError(fmt.Sprintf("the upgrade of database %q has not finished yet", name)).
			WithHint("the upgrade was requested and the platform keeps working on it\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting; the upgrade of database %q was requested and continues", name)).
			WithHint(check)
	}
	return cmdutil.MapWaitErr(err, noun, name, "wait")
}
