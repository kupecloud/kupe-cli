package database

import (
	"slices"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

// upgradeState builds a database as the API reports it around an upgrade:
// spec.version, the full version it runs, the generation and what the
// operator has observed, the phase, and the Ready condition. An
// UpgradeInProgress condition is added when inProgress is a reason.
func upgradeState(spec, current string, generation, observed int64, phase string, ready bool, reason, message, inProgress string) *client.Database {
	db := running("orders-db")
	db.Version = spec
	db.Generation = generation
	db.Status.CurrentVersion = current
	db.Status.ObservedGeneration = observed
	db.Status.Phase = phase
	status := "False"
	if ready {
		status = "True"
	}
	db.Status.Conditions = []client.Condition{{Type: "Ready", Status: status, Reason: reason, Message: message}}
	if inProgress != "" {
		db.Status.Conditions = append(db.Status.Conditions, client.Condition{Type: client.ConditionUpgradeInProgress, Status: "True", Reason: inProgress, Message: message})
	}
	return db
}

func patchedVersions(fake *clienttest.Fake) []string {
	var out []string
	for _, c := range fake.Calls {
		if strings.HasPrefix(c, "UpdateDatabase:") {
			out = append(out, c)
		}
	}
	return out
}

func TestUpgrade_Requests(t *testing.T) {
	notReady := &client.APIError{StatusCode: 409, Code: "DATABASE_UPGRADE_NOT_READY", Message: "the database has no backup from the last 26 hours"}
	inProgress := &client.APIError{StatusCode: 409, Code: "DATABASE_UPGRADE_IN_PROGRESS", Message: "an upgrade is already running"}
	refused := &client.APIError{StatusCode: 400, Code: client.DatabaseCodeMajorUpgradeUnavailable, Message: "major version upgrades are not available on this platform"}
	tests := []struct {
		name        string
		spec        string
		version     string
		updateErr   error
		wantExit    int
		wantVersion string
		wantErrText string
		wantHint    string
	}{
		{"sets the new major", "17", "18", nil, cli.ExitOK, "18", "", ""},
		{"the running major is a no-op", "18", "18", nil, cli.ExitOK, "18", "", ""},
		{"rejects a non-major version", "17", "18.1", nil, cli.ExitMisuse, "17", "major version", ""},
		{"a platform refusal exits 7 with the API's reason", "17", "18", refused, cli.ExitUnavailable, "17", refused.Message, ""},
		{"not ready exits 5 with the API's reason", "17", "18", notReady, cli.ExitConflict, "17", notReady.Message, "kupe db get orders-db"},
		{"in progress exits 5 and points at get", "17", "18", inProgress, cli.ExitConflict, "17", inProgress.Message, "kupe db get orders-db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			db := running("orders-db")
			db.Version = tt.spec
			db.Status.CurrentVersion = tt.spec + ".6"
			if tt.version == tt.spec {
				db.Status.CurrentVersion = tt.version + ".0"
			}
			fake.DatabasesByName["orders-db"] = db
			fake.UpdateDatabaseErr = tt.updateErr
			f := factoryWith(t, fake)
			_, _, err := run(f, newUpgradeCmd(f), "orders-db", "--version", tt.version, "--yes", "--wait=false")
			assertExit(t, err, tt.wantExit)
			if got := fake.DatabasesByName["orders-db"].Version; got != tt.wantVersion {
				t.Fatalf("version = %q; want %q", got, tt.wantVersion)
			}
			if tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("error %q missing %q", err, tt.wantErrText)
			}
			if tt.wantHint != "" && !strings.Contains(hint(err), tt.wantHint) {
				t.Errorf("hint %q missing %q", hint(err), tt.wantHint)
			}
			if tt.name == "the running major is a no-op" && len(patchedVersions(fake)) != 0 {
				t.Errorf("a no-op sent %v", patchedVersions(fake))
			}
		})
	}
}

func TestUpgrade_Flags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"neither version nor cancel", nil, "--version is required"},
		{"both", []string{"--version", "18", "--cancel"}, "cannot be used together"},
		{"cancel with an empty version", []string{"--version", "", "--cancel"}, "cannot be used together"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			_, _, err := run(f, newUpgradeCmd(f), append([]string{"orders-db"}, tt.args...)...)
			assertExit(t, err, cli.ExitMisuse)
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v; want %q", err, tt.want)
			}
			if len(fake.Calls) != 0 {
				t.Fatalf("calls = %v; want none", fake.Calls)
			}
		})
	}
}

func TestUpgrade_ConfirmsBeforeTakingTheDatabaseOffline(t *testing.T) {
	seed := func() *clienttest.Fake {
		fake := clienttest.New()
		fake.DatabasesByName["orders-db"] = upgradeState("17", "17.6", 1, 1, client.PhaseRunning, true, "DatabaseHealthy", "", "")
		return fake
	}
	t.Run("a non-interactive run without --yes refuses and sends nothing", func(t *testing.T) {
		fake := seed()
		f := factoryWith(t, fake)
		_, _, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "18")
		assertExit(t, err, cli.ExitMisuse)
		if !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("err = %v; want it to name --yes", err)
		}
		if got := patchedVersions(fake); len(got) != 0 {
			t.Fatalf("sent %v without confirmation", got)
		}
	})
	t.Run("the question names the database and both majors", func(t *testing.T) {
		fake := seed()
		f := factoryWith(t, fake)
		interactive(f, "yes\n")
		_, stderr, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "18", "--wait=false")
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		want := "This takes database orders-db offline while it upgrades from 17 to 18. Type yes to confirm: "
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q; want %q", stderr, want)
		}
		if fake.DatabasesByName["orders-db"].Version != "18" {
			t.Fatal("version not sent after yes")
		}
	})
	t.Run("any other answer aborts", func(t *testing.T) {
		fake := seed()
		f := factoryWith(t, fake)
		interactive(f, "no\n")
		_, _, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "18")
		assertExit(t, err, cli.ExitMisuse)
		if got := patchedVersions(fake); len(got) != 0 {
			t.Fatalf("sent %v after a no", got)
		}
	})
	t.Run("the running major asks nothing", func(t *testing.T) {
		fake := seed()
		f := factoryWith(t, fake)
		interactive(f, "")
		if _, stderr, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "17"); err != nil || stderr != "" {
			t.Fatalf("err = %v stderr = %q; want a silent no-op", err, stderr)
		}
	})
}

func TestUpgrade_Cancel(t *testing.T) {
	tests := []struct {
		name        string
		db          *client.Database
		wantVersion string
		wantExit    int
		wantErr     string
		updateErr   error
	}{
		{"sends the running major", upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeWaiting, "no recent backup", client.ReasonUpgradeWaiting), "17", cli.ExitOK, "", nil},
		{"while the pre-upgrade backup runs", upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonPreUpgradeBackup, "taking the backup", client.ReasonPreUpgradeBackup), "17", cli.ExitOK, "", nil},
		{"after a rollback", upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeRolledBack, "rolled back", ""), "17", cli.ExitOK, "", nil},
		{"after a failure", upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, client.ReasonUpgradeFailed, "failed", ""), "17", cli.ExitOK, "", nil},
		{"nothing to cancel", running("orders-db"), "18", cli.ExitMisuse, "nothing to cancel", nil},
		{"no running version reported", upgradeState("18", "", 2, 2, client.PhasePending, false, "Provisioning", "", ""), "18", cli.ExitMisuse, "does not report the version it runs", nil},
		{"once the database is offline the platform refuses", upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, false, client.ReasonUpgrading, "moving data", client.ReasonUpgrading), "18", cli.ExitConflict, "an upgrade is already running",
			&client.APIError{StatusCode: 409, Code: "DATABASE_UPGRADE_IN_PROGRESS", Message: "an upgrade is already running"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabasesByName["orders-db"] = tt.db
			fake.UpdateDatabaseErr = tt.updateErr
			f := factoryWith(t, fake)
			// Not interactive and no --yes: a cancel never asks.
			_, stderr, err := run(f, newUpgradeCmd(f), "orders-db", "--cancel")
			assertExit(t, err, tt.wantExit)
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v; want %q", err, tt.wantErr)
			}
			if got := fake.DatabasesByName["orders-db"].Version; got != tt.wantVersion {
				t.Errorf("version = %q; want %q", got, tt.wantVersion)
			}
			if strings.Contains(stderr, "Type yes") {
				t.Errorf("cancel asked for confirmation: %q", stderr)
			}
			if tt.wantExit == cli.ExitMisuse && len(patchedVersions(fake)) != 0 {
				t.Errorf("sent %v", patchedVersions(fake))
			}
		})
	}
}

// waitFake seeds the database as it is just after the PATCH and scripts the
// reads that follow: the command's own first read, the read-modify-write
// read, then the polls.
func waitFake(polls ...*client.Database) *clienttest.Fake {
	fake := clienttest.New()
	before := upgradeState("17", "17.6", 1, 1, client.PhaseRunning, true, "DatabaseHealthy", "", "")
	// The stored copy is what the PATCH changes; it carries the generation
	// the change produces.
	fake.DatabasesByName["orders-db"] = upgradeState("17", "17.6", 2, 1, client.PhaseRunning, true, "DatabaseHealthy", "", "")
	fake.GetDatabaseSeq["orders-db"] = append([]*client.Database{before, before}, polls...)
	return fake
}

func upgradeRun(t *testing.T, fake *clienttest.Fake, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	f := factoryWith(t, fake)
	args := append([]string{"orders-db", "--version", "18", "--yes", "--wait-timeout", "30s"}, extra...)
	return run(f, newUpgradeCmd(f), args...)
}

func TestUpgrade_WaitEndsOnTheNewMajor(t *testing.T) {
	fake := waitFake(
		// The operator has seen the change; the database is offline.
		upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, false, client.ReasonUpgrading, "upgrading", client.ReasonUpgrading),
		// Running on 18 with an HA standby still rebuilding: progress.
		upgradeState("18", "18.1", 2, 2, client.PhaseRunning, false, "DatabaseNotHealthy", "applying changes", ""),
		upgradeState("18", "18.1", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""),
	)
	stdout, stderr, err := upgradeRun(t, fake)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(stdout, "18.1") {
		t.Errorf("stdout = %q; want the upgraded database", stdout)
	}
	if !strings.Contains(stderr, "kupe database backup list orders-db") {
		t.Errorf("stderr = %q; want the backup list command", stderr)
	}
	if polls := countCalls(fake, "GetDatabase:orders-db"); polls != 5 {
		t.Fatalf("GETs = %d; want the two reads and three polls", polls)
	}
}

func TestUpgrade_WaitKeepsGoingThroughProgress(t *testing.T) {
	// The Ready reason of each step must not end the wait, even Degraded
	// ones with a grace of one nanosecond: waiting and backing up are the
	// platform's to resolve.
	shortGrace(t)
	for _, step := range []*client.Database{
		upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeWaiting, "waiting for a backup from the last 26 hours", client.ReasonUpgradeWaiting),
		upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonPreUpgradeBackup, "taking the backup", client.ReasonPreUpgradeBackup),
		upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, false, client.ReasonUpgrading, "upgrading", client.ReasonUpgrading),
		// Degraded while a check waits, or the backup runs, is the platform's
		// to resolve: the upgrade starts once the database is healthy.
		upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, client.ReasonUpgradeWaiting, "waiting for the database to be running", client.ReasonUpgradeWaiting),
		upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, client.ReasonPreUpgradeBackup, "taking the backup", client.ReasonPreUpgradeBackup),
	} {
		t.Run(step.Status.Conditions[0].Reason, func(t *testing.T) {
			fake := waitFake(step)
			_, _, err := upgradeRun(t, fake, "--wait-timeout", "1s")
			assertExit(t, err, cli.ExitTimeout)
		})
	}
}

// shortGrace makes the Degraded grace one poll long.
func shortGrace(t *testing.T) {
	t.Helper()
	old := upgradeDegradedGrace
	upgradeDegradedGrace = 1
	t.Cleanup(func() { upgradeDegradedGrace = old })
}

// addCondition appends a condition to db's status and returns db.
func addCondition(db *client.Database, typ, status, reason, message string) *client.Database {
	db.Status.Conditions = append(db.Status.Conditions, client.Condition{Type: typ, Status: status, Reason: reason, Message: message})
	return db
}

// heldV149 is the v1.49.0 switched-off hold: Running, Ready True and only
// VersionUnsupported True says so; inProgress is the UpgradeInProgress
// condition the operator left (status, reason), or "" for none.
func heldV149(inProgressStatus, inProgressReason string) *client.Database {
	db := upgradeState("18", "17.6", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", "")
	addCondition(db, client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "major 18 is not offered")
	if inProgressReason != "" {
		addCondition(db, client.ConditionUpgradeInProgress, inProgressStatus, inProgressReason, "")
	}
	return db
}

func TestUpgrade_WaitEndsWhenMajorsAreSwitchedOff(t *testing.T) {
	held551 := upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeSwitchedOff, "major upgrades are switched off on this platform; set the version back to 17", "")
	addCondition(held551, client.ConditionUpgradeInProgress, "False", client.ReasonUpgradeSwitchedOff, "")
	tests := []struct {
		name string
		db   *client.Database
		want string
	}{
		{"operator with the explicit reason: Ready False UpgradeSwitchedOff", held551, "set the version back to 17"},
		{"v1.49.0 with no upgrade recorded", heldV149("False", "NoUpgrade"), "major 18 is not offered"},
		{"v1.49.0 with a stale UpgradeWaiting", heldV149("True", client.ReasonUpgradeWaiting), "major 18 is not offered"},
		{"v1.49.0 with a stale UpgradeSucceeded", heldV149("False", client.ReasonUpgradeSucceeded), "major 18 is not offered"},
		{"v1.49.0 with no UpgradeInProgress at all", heldV149("", ""), "major 18 is not offered"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortGrace(t)
			_, _, err := upgradeRun(t, waitFake(tt.db))
			assertExit(t, err, cli.ExitUnavailable)
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v; want Ready's message %q", err, tt.want)
			}
			if h := hint(err); !strings.Contains(h, "kupe db upgrade orders-db --cancel") {
				t.Errorf("hint = %q; want the cancel command", h)
			}
		})
	}
}

// The stale VersionUnsupported of an earlier hold must not end a wait that
// is still working, or one that has finished.
func TestUpgrade_StaleVersionUnsupportedDoesNotEndTheWait(t *testing.T) {
	shortGrace(t)
	for _, tt := range []struct {
		name string
		db   *client.Database
	}{
		{"phase Upgrading", addCondition(upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, false, client.ReasonUpgrading, "upgrading", client.ReasonUpgrading),
			client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "stale")},
		{"phase Upgrading with Ready True", addCondition(upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, true, "DatabaseHealthy", "", ""),
			client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "stale")},
		{"Ready False PreUpgradeBackup", addCondition(upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonPreUpgradeBackup, "taking the backup", client.ReasonPreUpgradeBackup),
			client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "stale")},
		{"Ready False UpgradeWaiting", addCondition(upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeWaiting, "waiting", client.ReasonUpgradeWaiting),
			client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "stale")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := upgradeRun(t, waitFake(tt.db), "--wait-timeout", "1s")
			assertExit(t, err, cli.ExitTimeout)
		})
	}
	t.Run("a finished upgrade still succeeds", func(t *testing.T) {
		done := addCondition(upgradeState("18", "18.1", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""),
			client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "stale")
		if _, _, err := upgradeRun(t, waitFake(done)); err != nil {
			t.Fatalf("upgrade: %v", err)
		}
	})
}

// The window after a successful upgrade, and a stale hold of an earlier
// upgrade, as an older operator leaves them.
func TestUpgrade_CancelGuard(t *testing.T) {
	succeeded := func(spec, current string, gen, obs int64) *client.Database {
		judged := gen
		if gen == 3 {
			judged = 2 // an older judgement
		}
		db := upgradeState(spec, current, gen, obs, client.PhaseRunning, true, "DatabaseHealthy", "", "")
		addCondition(db, client.ConditionUpgradeInProgress, "False", client.ReasonUpgradeSucceeded, "")
		db.Status.Conditions[len(db.Status.Conditions)-1].ObservedGeneration = judged
		return db
	}
	tests := []struct {
		name     string
		db       *client.Database
		wantExit int
		wantText string
	}{
		{"after success, before currentVersion moved: refused", succeeded("18", "17.6", 2, 2), cli.ExitMisuse, "already finished"},
		{"an UpgradeSucceeded judged for an older generation: refused, neutrally", succeeded("18", "17.6", 3, 3), cli.ExitMisuse, "has not judged this upgrade"},
		{"the change is not applied yet: refused", upgradeState("18", "17.6", 3, 2, client.PhaseRunning, false, client.ReasonUpgradeWaiting, "w", client.ReasonUpgradeWaiting), cli.ExitMisuse, "try again in a moment"},
		{"nothing recorded to cancel: refused", upgradeState("18", "17.6", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""), cli.ExitMisuse, "no upgrade that is waiting"},
		{"the stale hold of an earlier upgrade: allowed", addCondition(succeeded("18", "17.6", 2, 2), client.ConditionVersionUnsupported, "True", client.ReasonMajorChangeUnsupported, "x"), cli.ExitOK, ""},
		{"Ready False UpgradeSwitchedOff: allowed", upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeSwitchedOff, "off", ""), cli.ExitOK, ""},
		{"UpgradeInProgress True UpgradeWaiting: allowed", upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, "x", "w", client.ReasonUpgradeWaiting), cli.ExitOK, ""},
		{"Ready False UpgradeFailed: allowed", upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, client.ReasonUpgradeFailed, "f", ""), cli.ExitOK, ""},
	}
	for _, how := range []struct {
		name string
		args []string
	}{{"--cancel", []string{"--cancel"}}, {"--version R", []string{"--version", "17"}}} {
		for _, tt := range tests {
			t.Run(how.name+" "+tt.name, func(t *testing.T) {
				fake := clienttest.New()
				fake.DatabasesByName["orders-db"] = tt.db
				f := factoryWith(t, fake)
				interactive(f, "")
				_, _, err := run(f, newUpgradeCmd(f), append([]string{"orders-db"}, how.args...)...)
				assertExit(t, err, tt.wantExit)
				if tt.wantExit != cli.ExitOK {
					if !strings.Contains(err.Error()+hint(err), tt.wantText) {
						t.Errorf("err = %v hint = %q; want %q", err, hint(err), tt.wantText)
					}
					if got := patchedVersions(fake); len(got) != 0 {
						t.Errorf("sent %v", got)
					}
					return
				}
				if fake.DatabasesByName["orders-db"].Version != "17" {
					t.Error("the cancel was not sent")
				}
			})
		}
	}
}

func TestUpgrade_WaitEndsOnAPersistentDegradedPhase(t *testing.T) {
	shortGrace(t)
	t.Run("after the upgrade succeeded", func(t *testing.T) {
		db := upgradeState("18", "18.1", 2, 2, client.PhaseDegraded, false, "DatabaseNotHealthy", "no instance is ready", "")
		db.Status.Conditions = append(db.Status.Conditions, client.Condition{Type: client.ConditionUpgradeInProgress, Status: "False", Reason: client.ReasonUpgradeSucceeded})
		_, _, err := upgradeRun(t, waitFake(db))
		assertExit(t, err, cli.ExitGeneral)
		if !strings.Contains(err.Error(), "was upgraded to 18 but is not healthy") || !strings.Contains(err.Error(), "no instance is ready") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("for any other reason", func(t *testing.T) {
		_, _, err := upgradeRun(t, waitFake(upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, "QuotaExceeded", "no quota left", "")))
		assertExit(t, err, cli.ExitGeneral)
		if !strings.Contains(err.Error(), "Degraded") || !strings.Contains(err.Error(), "no quota left") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("not before the operator has applied the request", func(t *testing.T) {
		fake := waitFake(upgradeState("18", "17.6", 2, 1, client.PhaseDegraded, false, "QuotaExceeded", "old", ""))
		_, _, err := upgradeRun(t, fake, "--wait-timeout", "1s")
		assertExit(t, err, cli.ExitTimeout)
	})
}

func TestUpgrade_DegradedWithinTheGraceKeepsWaiting(t *testing.T) {
	// The grace is the default 5 minutes here, so a short Degraded spell ends well.
	fake := waitFake(
		upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, "DatabaseNotHealthy", "restarting", ""),
		upgradeState("18", "18.1", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""),
	)
	if _, _, err := upgradeRun(t, fake); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
}

func TestUpgrade_WaitEndsWhenAnotherRequestChangedTheVersion(t *testing.T) {
	// Someone cancelled from elsewhere: the database is back on 17, Ready.
	fake := waitFake(upgradeState("17", "17.6", 3, 3, client.PhaseRunning, true, "DatabaseHealthy", "", ""))
	_, _, err := upgradeRun(t, fake)
	assertExit(t, err, cli.ExitGeneral)
	if !strings.Contains(err.Error(), "was changed to 17") || !strings.Contains(err.Error(), "no longer requested") {
		t.Errorf("err = %v", err)
	}
}

func TestUpgrade_RunningMajorAndLower(t *testing.T) {
	seed := func(spec string) *clienttest.Fake {
		fake := clienttest.New()
		fake.DatabasesByName["orders-db"] = upgradeState(spec, "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeWaiting, "waiting", client.ReasonUpgradeWaiting)
		return fake
	}
	t.Run("the running major cancels a pending upgrade without the offline question", func(t *testing.T) {
		fake := seed("18")
		f := factoryWith(t, fake)
		interactive(f, "")
		_, stderr, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "17")
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		if strings.Contains(stderr, "offline") || !strings.Contains(stderr, "set back to major 17, the one it runs") || strings.Contains(stderr, "cancelled") {
			t.Errorf("stderr = %q", stderr)
		}
		if fake.DatabasesByName["orders-db"].Version != "17" {
			t.Error("the version was not set back")
		}
	})
	t.Run("below the running major is refused before any question", func(t *testing.T) {
		fake := seed("18")
		f := factoryWith(t, fake)
		interactive(f, "")
		_, stderr, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "16")
		assertExit(t, err, cli.ExitMisuse)
		if strings.Contains(stderr, "Type yes") || len(patchedVersions(fake)) != 0 {
			t.Errorf("stderr = %q calls = %v", stderr, fake.Calls)
		}
	})
}

func TestUpgrade_CancelTooLateSaysSo(t *testing.T) {
	fake := clienttest.New()
	fake.DatabasesByName["orders-db"] = upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, false, client.ReasonUpgrading, "moving data", client.ReasonUpgrading)
	fake.UpdateDatabaseErr = &client.APIError{StatusCode: 409, Code: "DATABASE_UPGRADE_IN_PROGRESS", Message: "an upgrade is already running"}
	f := factoryWith(t, fake)
	_, _, err := run(f, newUpgradeCmd(f), "orders-db", "--cancel")
	assertExit(t, err, cli.ExitConflict)
	if h := hint(err); !strings.Contains(h, "can be cancelled") || strings.Contains(h, "db wait") {
		t.Errorf("hint = %q", h)
	}
}

func TestUpgrade_FailedHintDoesNotPromiseARetry(t *testing.T) {
	fake := waitFake(upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, client.ReasonUpgradeFailed, "could not be returned to 17", ""))
	_, _, err := upgradeRun(t, fake)
	if h := hint(err); strings.Contains(h, "try again") {
		t.Errorf("hint = %q", h)
	}
}

func TestUpgrade_WaitIsNotFooledByARunningOldMajor(t *testing.T) {
	// Ready True, observedGeneration caught up, but still on 17: not success.
	fake := waitFake(upgradeState("18", "17.6", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""))
	_, _, err := upgradeRun(t, fake, "--wait-timeout", "1s")
	assertExit(t, err, cli.ExitTimeout)
}

func TestUpgrade_WaitIgnoresStatusTheOperatorHasNotSeenYet(t *testing.T) {
	// A rolled-back reason left by an earlier attempt, read before the
	// operator has applied this request, must not end the wait.
	fake := waitFake(
		upgradeState("18", "17.6", 2, 1, client.PhaseRunning, false, client.ReasonUpgradeRolledBack, "left over", ""),
		upgradeState("18", "18.1", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""),
	)
	if _, _, err := upgradeRun(t, fake); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	// Likewise a Ready True on the new major read before it caught up.
	fake = waitFake(
		upgradeState("18", "18.1", 2, 1, client.PhaseRunning, true, "DatabaseHealthy", "", ""),
		upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeRolledBack, "rolled back", ""),
	)
	_, _, err := upgradeRun(t, fake)
	assertExit(t, err, cli.ExitGeneral)
}

func TestUpgrade_WaitEndsWhenRolledBack(t *testing.T) {
	fake := waitFake(upgradeState("18", "17.6", 2, 2, client.PhaseRunning, false, client.ReasonUpgradeRolledBack, "the check failed before any data changed", ""))
	stdout, _, err := upgradeRun(t, fake)
	assertExit(t, err, cli.ExitGeneral)
	if !strings.Contains(err.Error(), "rolled back") || !strings.Contains(err.Error(), "the check failed before any data changed") {
		t.Errorf("err = %v; want the rollback and the platform's message", err)
	}
	if h := hint(err); !strings.Contains(h, "kupe db upgrade orders-db --cancel") || !strings.Contains(h, "still runs its old major") {
		t.Errorf("hint = %q; want the cancel command and the state", h)
	}
	if stdout != "" {
		t.Errorf("stdout = %q; want nothing for a failed upgrade", stdout)
	}
}

func TestUpgrade_WaitEndsWhenFailed(t *testing.T) {
	fake := waitFake(upgradeState("18", "17.6", 2, 2, client.PhaseDegraded, false, client.ReasonUpgradeFailed, "the database still runs 17", ""))
	_, _, err := upgradeRun(t, fake)
	assertExit(t, err, cli.ExitGeneral)
	if !strings.Contains(err.Error(), "failed") || !strings.Contains(err.Error(), "the database still runs 17") {
		t.Errorf("err = %v; want the failure and the platform's message", err)
	}
	if h := hint(err); !strings.Contains(h, "kupe database backup list orders-db") {
		t.Errorf("hint = %q; want the backup list command", h)
	}
}

func TestUpgrade_WaitEndsWhenTheDatabaseIsBeingDeleted(t *testing.T) {
	fake := waitFake(upgradeState("18", "17.6", 2, 2, client.PhaseTerminating, false, "Terminating", "", ""))
	_, _, err := upgradeRun(t, fake)
	assertExit(t, err, cli.ExitGeneral)
	if !strings.Contains(err.Error(), "Terminating") {
		t.Errorf("err = %v", err)
	}
}

func TestUpgrade_NoWaitReturnsAtOnce(t *testing.T) {
	fake := waitFake()
	stdout, stderr, err := upgradeRun(t, fake, "--wait=false")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(stdout, "orders-db") || stderr != "" {
		t.Errorf("stdout = %q stderr = %q", stdout, stderr)
	}
	if polls := countCalls(fake, "GetDatabase:orders-db"); polls != 2 {
		t.Fatalf("GETs = %d; want only the two reads", polls)
	}
}

func TestUpgrade_AlreadyRequestedWaitsWithoutResending(t *testing.T) {
	fake := clienttest.New()
	asked := upgradeState("18", "17.6", 2, 2, client.PhaseUpgrading, false, client.ReasonUpgrading, "upgrading", client.ReasonUpgrading)
	fake.DatabasesByName["orders-db"] = asked
	fake.GetDatabaseSeq["orders-db"] = []*client.Database{
		asked,
		upgradeState("18", "18.0", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""),
	}
	f := factoryWith(t, fake)
	interactive(f, "")
	if _, _, err := run(f, newUpgradeCmd(f), "orders-db", "--version", "18", "--wait-timeout", "30s"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if got := patchedVersions(fake); len(got) != 0 {
		t.Fatalf("resent the version: %v", got)
	}
}

func TestUpgrade_CallsAreInOrder(t *testing.T) {
	fake := waitFake(upgradeState("18", "18.1", 2, 2, client.PhaseRunning, true, "DatabaseHealthy", "", ""))
	if _, _, err := upgradeRun(t, fake); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	want := []string{"GetDatabase:orders-db", "GetDatabase:orders-db", "UpdateDatabase:orders-db", "GetDatabase:orders-db"}
	got := slices.DeleteFunc(slices.Clone(fake.Calls), func(c string) bool { return c == "Tenant" })
	if !slices.Equal(got, want) {
		t.Fatalf("calls = %v; want %v", got, want)
	}
}
