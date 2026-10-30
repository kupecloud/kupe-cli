package database

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

// completedBackup is a Completed manual backup of database, taken when it
// had 18 and 10Gi.
func completedBackup(name, database string) *client.Backup {
	return &client.Backup{
		Name: name, Database: database, Reason: "Manual", CreatedAt: "2026-10-30T14:15:00Z",
		Status: &client.BackupStatus{
			Phase: client.PhaseCompleted, CompletedAt: "2026-10-30T14:17:00Z", SizeBytes: 1610612736,
			Source:     &client.BackupSource{Name: database, Version: "18", Storage: "10Gi"},
			Conditions: []client.Condition{{Type: "Ready", Status: "True", Reason: "Completed"}},
		},
	}
}

// unknownRouteErr is the error a kupe-api that predates manual backups
// returns: Go's plain-text 404 for a route it does not serve.
func unknownRouteErr(t *testing.T) error {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := client.New(srv.URL, "acme", "kupe_test", "kupe-cli-test/0.0.0").ListBackups(context.Background(), "")
	if !client.IsUnknownRoute(err) {
		t.Fatalf("want an unknown-route error, got %v", err)
	}
	return err
}

func TestBackupCreate_SendsTheNameAndWaitsForCompleted(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantName string
		wantSent client.CreateBackupRequest
	}{
		{"a name is sent", []string{"orders-db", "--name", "before-migration"}, "before-migration",
			client.CreateBackupRequest{Name: "before-migration"}},
		{"no name leaves it to the platform", []string{"orders-db"}, "orders-db-backup", client.CreateBackupRequest{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabasesByName["orders-db"] = running("orders-db")
			inProgress := completedBackup(tt.wantName, "orders-db")
			inProgress.Status = &client.BackupStatus{Phase: "Running"}
			fake.GetBackupSeq[tt.wantName] = []*client.Backup{inProgress, completedBackup(tt.wantName, "orders-db")}
			f := factoryWith(t, fake)
			out, _, err := run(f, newBackupCreateCmd(f), append(tt.args, "--wait-timeout", "30s")...)
			if err != nil {
				t.Fatalf("backup create: %v", err)
			}
			if !reflect.DeepEqual(fake.BackupCreates, []client.CreateBackupRequest{tt.wantSent}) {
				t.Fatalf("sent %+v; want %+v", fake.BackupCreates, tt.wantSent)
			}
			if v, _ := detailValue(out, "Phase"); v != "Completed" {
				t.Fatalf("final render should be the Completed backup:\n%s", out)
			}
		})
	}
}

func TestBackupCreate_WaitFalseReturnsThePendingBackup(t *testing.T) {
	fake := clienttest.New()
	fake.DatabasesByName["orders-db"] = running("orders-db")
	f := factoryWith(t, fake)
	out, _, err := run(f, newBackupCreateCmd(f), "orders-db", "--name", "before-migration", "--wait=false", "-o", "json")
	if err != nil {
		t.Fatalf("backup create: %v", err)
	}
	var b client.Backup
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("stdout is not a backup: %v\n%s", err, out)
	}
	if b.Name != "before-migration" || b.Status.Phase != client.PhasePending {
		t.Fatalf("backup = %+v", b)
	}
	assertNoCall(t, fake, "GetBackup")
}

func TestBackupCreate_FailedExits1WithThePlatformsReason(t *testing.T) {
	fake := clienttest.New()
	fake.DatabasesByName["orders-db"] = running("orders-db")
	failed := completedBackup("before-migration", "orders-db")
	failed.Status = &client.BackupStatus{Phase: client.PhaseFailed,
		Conditions: []client.Condition{{Type: "Ready", Status: "False", Reason: "Failed", Message: "the backup could not be written"}}}
	fake.GetBackupSeq["before-migration"] = []*client.Backup{failed}
	f := factoryWith(t, fake)
	_, _, err := run(f, newBackupCreateCmd(f), "orders-db", "--name", "before-migration", "--wait-timeout", "30s")
	assertExit(t, err, cli.ExitGeneral)
	if want := `backup "before-migration" failed: the backup could not be written`; err.Error() != want {
		t.Fatalf("message = %q; want %q", err.Error(), want)
	}
	if h := hint(err); !strings.Contains(h, "kupe database backup delete before-migration") {
		t.Errorf("hint = %q; want the delete command", h)
	}
}

func TestBackupCreate_WaitTimeoutSaysItIsStillBeingTaken(t *testing.T) {
	fake := clienttest.New()
	fake.DatabasesByName["orders-db"] = running("orders-db")
	f := factoryWith(t, fake)
	_, _, err := run(f, newBackupCreateCmd(f), "orders-db", "--name", "before-migration", "--wait-timeout", "300ms")
	assertExit(t, err, cli.ExitTimeout)
	if want := `backup "before-migration" is still Pending`; err.Error() != want {
		t.Fatalf("message = %q; want %q", err.Error(), want)
	}
	if h := hint(err); !strings.Contains(h, "kupe database backup get before-migration") {
		t.Errorf("hint = %q; want the get command", h)
	}
}

func TestBackupCreate_Refusals(t *testing.T) {
	tests := []struct {
		name     string
		err      func(t *testing.T) error
		wantExit int
		wantMsg  string
		wantHint string
	}{
		{"database not running", func(*testing.T) error {
			return &client.APIError{StatusCode: 409, Code: client.BackupCodeDatabaseNotReady, Message: `database "orders-db" is not running`}
		}, cli.ExitConflict, `database "orders-db" is not running`, "kupe database wait orders-db"},
		{"20 backups already", func(*testing.T) error {
			return &client.APIError{StatusCode: 409, Code: client.BackupCodeLimitReached, Message: `database "orders-db" already has 20 manual backups`}
		}, cli.ExitConflict, `database "orders-db" already has 20 manual backups`, "kupe database backup list orders-db"},
		{"backups switched off", func(*testing.T) error {
			return &client.APIError{StatusCode: 400, Code: client.DatabaseCodeBackupsUnavailable, Message: "backups are not available on this platform"}
		}, cli.ExitUnavailable, "backups are not available on this platform", "existing manual backups can still be listed and deleted"},
		{"databases switched off", func(*testing.T) error {
			return &client.APIError{StatusCode: 404, Code: client.DatabaseCodeUnavailable, Message: "databases are not available on this platform"}
		}, cli.ExitNotFound, "databases are not available on this platform", "manual backups can still be listed and deleted"},
		{"creation paused", func(*testing.T) error {
			return &client.APIError{StatusCode: 400, Code: client.CodeCreationPaused,
				Message: "Database creation is temporarily paused while we expand platform capacity. Please try again later."}
		}, cli.ExitUnavailable, "Database creation is temporarily paused while we expand platform capacity. Please try again later.", "retry later"},
		{"database being deleted", func(*testing.T) error {
			return &client.APIError{StatusCode: 409, Code: client.CodeTerminating, Message: `database "orders-db" is being deleted`}
		}, cli.ExitConflict, `database "orders-db" is being deleted`, "kupe database wait orders-db --for deleted"},
		{"a kupe-api without manual backups", unknownRouteErr,
			cli.ExitUnavailable, "this platform does not offer database backups yet", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.CreateDatabaseBackupErr = tt.err(t)
			f := factoryWith(t, fake)
			_, _, err := run(f, newBackupCreateCmd(f), "orders-db")
			assertExit(t, err, tt.wantExit)
			if err.Error() != tt.wantMsg {
				t.Fatalf("message = %q; want %q", err.Error(), tt.wantMsg)
			}
			if h := hint(err); (tt.wantHint == "") != (h == "") || !strings.Contains(h, tt.wantHint) {
				t.Fatalf("hint = %q; want %q", h, tt.wantHint)
			}
		})
	}
}

func TestBackupCreate_MissingDatabaseExits4(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)
	_, _, err := run(f, newBackupCreateCmd(f), "ghost-db")
	assertExit(t, err, cli.ExitNotFound)
}

func seedBackups() *clienttest.Fake {
	fake := clienttest.New()
	fake.DatabasesByName["orders-db"] = running("orders-db")
	fake.Backups["orders-db-before-migration"] = completedBackup("orders-db-before-migration", "orders-db")
	gone := completedBackup("users-db-20261029-020000", "users-db")
	gone.Status.DatabaseDeleted = true
	gone.Status.SizeBytes = 0
	fake.Backups["users-db-20261029-020000"] = gone
	return fake
}

func TestBackupList(t *testing.T) {
	t.Run("without a database lists every backup and marks deleted databases", func(t *testing.T) {
		fake := seedBackups()
		f := factoryWith(t, fake)
		out, _, err := run(f, newBackupListCmd(f))
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 3 {
			t.Fatalf("want a header and two rows:\n%s", out)
		}
		for i, want := range [][]string{
			{"NAME", "DATABASE", "REASON", "PHASE", "SIZE", "COMPLETED", "AGE"},
			{"orders-db-before-migration", "orders-db ", "Manual", "Completed", "1.5 GiB"},
			{"users-db-20261029-020000", "users-db (deleted)", "Manual", "Completed"},
		} {
			for _, cell := range want {
				if !strings.Contains(lines[i], cell) {
					t.Errorf("row %d should contain %q:\n%s", i, cell, lines[i])
				}
			}
		}
		if strings.Contains(lines[1], "(deleted)") {
			t.Errorf("a live database is marked deleted:\n%s", lines[1])
		}
		if !hasCall(fake, "ListBackups:") {
			t.Errorf("calls = %v; want ListBackups with no database", fake.Calls)
		}
	})
	t.Run("with a database lists its backups, deleted databases' included", func(t *testing.T) {
		fake := seedBackups()
		f := factoryWith(t, fake)
		out, _, err := run(f, newBackupListCmd(f), "users-db", "-o", "json")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var got []client.Backup
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("stdout is not a JSON list: %v\n%s", err, out)
		}
		if len(got) != 1 || got[0].Name != "users-db-20261029-020000" || !got[0].Status.DatabaseDeleted {
			t.Fatalf("backups = %+v", got)
		}
		if !hasCall(fake, "ListBackups:users-db") {
			t.Errorf("calls = %v; want ListBackups for users-db", fake.Calls)
		}
	})
	t.Run("a kupe-api without manual backups exits 7", func(t *testing.T) {
		fake := clienttest.New()
		fake.ListBackupsErr = unknownRouteErr(t)
		f := factoryWith(t, fake)
		_, _, err := run(f, newBackupListCmd(f))
		assertExit(t, err, cli.ExitUnavailable)
		if err.Error() != "this platform does not offer database backups yet" {
			t.Fatalf("message = %q", err.Error())
		}
	})
	t.Run("more than one database is a misuse", func(t *testing.T) {
		f := factoryWith(t, seedBackups())
		_, _, err := run(f, newBackupListCmd(f), "orders-db", "users-db")
		if err == nil {
			t.Fatal("want an error for two databases")
		}
	})
}

func TestBackupGet(t *testing.T) {
	t.Run("shows the backup and what a restore needs", func(t *testing.T) {
		f := factoryWith(t, seedBackups())
		out, _, err := run(f, newBackupGetCmd(f), "users-db-20261029-020000")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		for field, want := range map[string]string{
			"Database": "users-db (deleted)", "Reason": "Manual", "Phase": "Completed",
			"Version": "18", "Storage": "10Gi", "Size": "",
		} {
			if got, ok := detailValue(out, field); !ok || got != want {
				t.Errorf("%s = %q (present %v); want %q\n%s", field, got, ok, want, out)
			}
		}
		if got, _ := detailValue(out, "Completed"); !strings.HasPrefix(got, "2026-10-30T14:17:00Z") {
			t.Errorf("Completed = %q", got)
		}
	})
	t.Run("a missing backup exits 4 with the API's message", func(t *testing.T) {
		f := factoryWith(t, seedBackups())
		_, _, err := run(f, newBackupGetCmd(f), "ghost")
		assertExit(t, err, cli.ExitNotFound)
		if err.Error() != `database backup "ghost" not found` {
			t.Fatalf("message = %q", err.Error())
		}
	})
}

func TestBackupDelete(t *testing.T) {
	t.Run("refused without --yes non-interactively", func(t *testing.T) {
		fake := seedBackups()
		f := factoryWith(t, fake)
		_, _, err := run(f, newBackupDeleteCmd(f), "orders-db-before-migration")
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "DeleteBackup")
	})
	t.Run("a wrong answer at the prompt deletes nothing", func(t *testing.T) {
		fake := seedBackups()
		f := factoryWith(t, fake)
		interactive(f, "no\n")
		_, _, err := run(f, newBackupDeleteCmd(f), "orders-db-before-migration")
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "DeleteBackup")
	})
	for _, tt := range []struct {
		name   string
		args   []string
		answer string
	}{
		{"confirmed at the prompt", nil, "yes\n"},
		{"--yes", []string{"--yes"}, ""},
	} {
		t.Run("deleted when "+tt.name, func(t *testing.T) {
			fake := seedBackups()
			f := factoryWith(t, fake)
			if tt.answer != "" {
				interactive(f, tt.answer)
			}
			out, stderr, err := run(f, newBackupDeleteCmd(f), append([]string{"orders-db-before-migration"}, tt.args...)...)
			if err != nil {
				t.Fatalf("delete: %v", err)
			}
			if _, still := fake.Backups["orders-db-before-migration"]; still {
				t.Fatal("backup still present")
			}
			if out != "backup/orders-db-before-migration delete requested\n" {
				t.Fatalf("stdout = %q", out)
			}
			if tt.answer != "" && !strings.Contains(stderr, `This will delete database backup "orders-db-before-migration"`) {
				t.Errorf("prompt = %q", stderr)
			}
		})
	}
	t.Run("a missing backup exits 4", func(t *testing.T) {
		f := factoryWith(t, seedBackups())
		_, _, err := run(f, newBackupDeleteCmd(f), "ghost", "--yes")
		assertExit(t, err, cli.ExitNotFound)
	})
}

func hasCall(fake *clienttest.Fake, call string) bool {
	for _, c := range fake.Calls {
		if c == call {
			return true
		}
	}
	return false
}
