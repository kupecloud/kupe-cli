package database

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

func factoryWith(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	f := cli.NewFactory(io, &cli.GlobalFlags{ConfigPath: cfgPath})
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

// run executes cmd and returns its error with captured stdout and stderr.
func run(f *cli.Factory, cmd *cobra.Command, args ...string) (stdout, stderr string, err error) {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err = cmd.Execute()
	return f.IOStreams.Out.(interface{ String() string }).String(),
		f.IOStreams.ErrOut.(interface{ String() string }).String(), err
}

// interactive turns prompts on and feeds answer to stdin.
func interactive(f *cli.Factory, answer string) {
	f.IOStreams.PromptsEnabled = true
	f.IOStreams.In = strings.NewReader(answer)
}

func running(name string) *client.Database {
	return &client.Database{
		Name: name, Engine: "postgres", Version: "18", DeletionProtection: true,
		Resources: &client.DatabaseResources{CPU: "500m", Memory: "1Gi", Storage: "10Gi"},
		Status:    &client.DatabaseStatus{Phase: client.PhaseRunning, CurrentVersion: "18.0", InstancesReady: 1, InstancesDesired: 1},
	}
}

func hint(err error) string {
	var e *cli.Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}

func assertExit(t *testing.T, err error, want int) {
	t.Helper()
	if code := cli.ExitCode(err); code != want {
		t.Fatalf("exit = %d; want %d (err: %v)", code, want, err)
	}
}

func assertNoCall(t *testing.T, fake *clienttest.Fake, prefix string) {
	t.Helper()
	for _, c := range fake.Calls {
		if strings.HasPrefix(c, prefix) {
			t.Fatalf("unexpected call %q in %v", c, fake.Calls)
		}
	}
}

func TestList_RendersTableColumnsAndFormats(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
		not  []string
	}{
		{"table shows the default columns", nil, []string{"NAME", "ENGINE", "VERSION", "RUNNING", "PHASE", "HA", "AGE", "orders-db", "18.0", "Running", "off"}, []string{"STORAGE"}},
		{"wide adds resources and protection", []string{"-o", "wide"}, []string{"STORAGE", "INSTANCES", "PROTECTED", "10Gi", "1/1", "on"}, nil},
		{"name prints one name per line", []string{"-o", "name"}, []string{"orders-db\n"}, []string{"NAME"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			f := factoryWith(t, fake)
			out, _, err := run(f, newListCmd(f), tt.args...)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(out, n) {
					t.Errorf("unexpected %q in:\n%s", n, out)
				}
			}
		})
	}
}

func TestGet(t *testing.T) {
	t.Run("json carries the full resource", func(t *testing.T) {
		fake := clienttest.New()
		fake.Databases["orders-db"] = running("orders-db")
		f := factoryWith(t, fake)
		out, _, err := run(f, newGetCmd(f), "orders-db", "-o", "json")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		var got client.Database
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if got.Name != "orders-db" || got.Status.CurrentVersion != "18.0" || !got.DeletionProtection {
			t.Fatalf("decoded = %+v", got)
		}
	})
	t.Run("details show version, instances and protection", func(t *testing.T) {
		fake := clienttest.New()
		fake.Databases["orders-db"] = running("orders-db")
		f := factoryWith(t, fake)
		out, _, err := run(f, newGetCmd(f), "orders-db")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		for _, w := range []string{"18 (running 18.0)", "Instances:", "1/1", "Deletion Protection:"} {
			if !strings.Contains(out, w) {
				t.Errorf("missing %q in:\n%s", w, out)
			}
		}
	})
	t.Run("missing database exits 4", func(t *testing.T) {
		f := factoryWith(t, clienttest.New())
		_, _, err := run(f, newGetCmd(f), "ghost")
		assertExit(t, err, cli.ExitNotFound)
	})
}

func TestCommands_DatabasesUnavailableSaysSo(t *testing.T) {
	fake := clienttest.New()
	fake.ListDatabasesErr = &client.APIError{StatusCode: 404, Code: client.DatabaseCodeUnavailable, Message: "managed databases are not enabled"}
	f := factoryWith(t, fake)
	_, _, err := run(f, newListCmd(f))
	assertExit(t, err, cli.ExitNotFound)
	if !strings.Contains(err.Error(), "managed databases are not available on this platform") ||
		!strings.Contains(err.Error(), "managed databases are not enabled") {
		t.Fatalf("message should explain and keep the API text: %v", err)
	}
	var ce *cli.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "existing databases can still be listed, unprotected (--deletion-protection=false) and deleted") {
		t.Fatalf("hint should name the clean-up that still works: %v", err)
	}
}

func TestCreate_Validation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"cpu is required", []string{"--memory", "1Gi", "--storage", "10Gi"}, "--cpu is required"},
		{"memory is required", []string{"--cpu", "1", "--storage", "10Gi"}, "--memory is required"},
		{"storage is required", []string{"--cpu", "1", "--memory", "1Gi"}, "--storage is required"},
		{"cpu has no unit", []string{"--cpu", "1Gi", "--memory", "1Gi", "--storage", "10Gi"}, "--cpu has an invalid format"},
		{"fractional millicores are refused", []string{"--cpu", "1.5m", "--memory", "1Gi", "--storage", "10Gi"}, "--cpu has an invalid format"},
		{"cpu longer than 20 characters", []string{"--cpu", strings.Repeat("1", 21), "--memory", "1Gi", "--storage", "10Gi"}, "--cpu is longer than 20 characters"},
		{"memory needs a unit", []string{"--cpu", "1", "--memory", "1024", "--storage", "10Gi"}, "--memory must include a unit"},
		{"storage needs a unit", []string{"--cpu", "1", "--memory", "1Gi", "--storage", "10"}, "--storage must include a unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			// --wait=false: a value that slips past validation must fail
			// the assertions below, not block on a phase the fake never sets.
			_, _, err := run(f, newCreateCmd(f), append([]string{"orders-db", "--wait=false"}, tt.args...)...)
			assertExit(t, err, cli.ExitMisuse)
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v; want %q", err, tt.want)
			}
			assertNoCall(t, fake, "CreateDatabase")
		})
	}
}

func TestCreate_SendsFlags(t *testing.T) {
	base := []string{"orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--wait=false"}
	tests := []struct {
		name          string
		extra         []string
		wantVersion   string
		wantHA        bool
		wantProtected bool
		wantStderr    string
	}{
		{"defaults keep protection on and leave the version to the platform", nil, "", false, true, ""},
		{"explicit version and HA, with an HA billing note", []string{"--version", "17", "--ha"}, "17", true, true, "two instances"},
		{"--no-deletion-protection turns protection off", []string{"--no-deletion-protection"}, "", false, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			out, stderr, err := run(f, newCreateCmd(f), append(base, tt.extra...)...)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			db := fake.Databases["orders-db"]
			if db == nil {
				t.Fatal("database not created")
			}
			if db.Engine != "postgres" || db.Version != tt.wantVersion || db.HighAvailability != tt.wantHA ||
				db.DeletionProtection != tt.wantProtected || db.Resources.CPU != "500m" {
				t.Fatalf("created = %+v", db)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr missing %q:\n%s", tt.wantStderr, stderr)
			}
			if !strings.Contains(out, "orders-db") {
				t.Errorf("stdout should render the database:\n%s", out)
			}
		})
	}
}

func TestCreate_WaitsForRunning(t *testing.T) {
	fake := clienttest.New()
	fake.GetDatabaseSeq["orders-db"] = []*client.Database{
		{Name: "orders-db", Status: &client.DatabaseStatus{Phase: client.PhaseProvisioning}},
		running("orders-db"),
	}
	f := factoryWith(t, fake)
	out, _, err := run(f, newCreateCmd(f), "orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--wait-timeout", "30s")
	if err != nil {
		t.Fatalf("create --wait: %v", err)
	}
	if !strings.Contains(out, "Running") {
		t.Fatalf("final render should be the Running database:\n%s", out)
	}
}

// TestCreate_DegradedExits1WithGetHint pins that a database reaching
// Degraded during the create wait is the database's failure (exit 1, naming
// the database and its phase, pointing at "kupe database get"), not lost
// contact with kupe-api (exit 7).
func TestCreate_DegradedExits1WithGetHint(t *testing.T) {
	fake := clienttest.New()
	fake.GetDatabaseSeq["orders-db"] = []*client.Database{
		{Name: "orders-db", Status: &client.DatabaseStatus{Phase: client.PhaseProvisioning}},
		{Name: "orders-db", Status: &client.DatabaseStatus{Phase: client.PhaseDegraded}},
	}
	f := factoryWith(t, fake)
	_, _, err := run(f, newCreateCmd(f), "orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--wait-timeout", "5s")
	assertExit(t, err, cli.ExitGeneral)
	if msg := err.Error(); !strings.Contains(msg, `database "orders-db" entered Degraded state`) || strings.Contains(msg, "lost contact") {
		t.Errorf("message = %q; want the Degraded phase, not lost contact", msg)
	}
	if h := hint(err); !strings.Contains(h, "kupe database get orders-db") {
		t.Errorf("hint = %q; want the get command", h)
	}
}

func TestCreate_PausedCreationExits7(t *testing.T) {
	tests := []struct {
		name string
		err  *client.APIError
	}{
		{"creation_paused code", &client.APIError{StatusCode: 400, Code: client.CodeCreationPaused,
			Message: "Database creation is temporarily paused while we expand platform capacity. Please try again later."}},
		{"older server: API wording without a code", &client.APIError{StatusCode: 400,
			Message: "Database creation is temporarily paused while we expand platform capacity. Please try again later."}},
		{"older server: lower-case webhook wording", &client.APIError{StatusCode: 400,
			Message: "database creation is temporarily paused while we expand platform capacity, please try again later"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.CreateDatabaseErr = tt.err
			f := factoryWith(t, fake)
			_, _, err := run(f, newCreateCmd(f), "orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--wait=false")
			assertExit(t, err, cli.ExitUnavailable)
			if !strings.Contains(err.Error(), "database creation is paused") || !strings.Contains(err.Error(), tt.err.Message) {
				t.Fatalf("should say creation is paused and keep the API message: %v", err)
			}
		})
	}
}

func TestCreate_ConflictExits5(t *testing.T) {
	fake := clienttest.New()
	fake.Databases["orders-db"] = running("orders-db")
	f := factoryWith(t, fake)
	_, _, err := run(f, newCreateCmd(f), "orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "10Gi", "--wait=false")
	assertExit(t, err, cli.ExitConflict)
}

func TestUpdate_PatchesOnlyPassedFlags(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(*client.Database) bool
	}{
		{"storage alone keeps cpu and memory", []string{"--storage", "20Gi"}, func(db *client.Database) bool {
			return db.Resources.Storage == "20Gi" && db.Resources.CPU == "500m" && db.Resources.Memory == "1Gi" && !db.HighAvailability
		}},
		{"--ha turns high availability on", []string{"--ha"}, func(db *client.Database) bool { return db.HighAvailability && db.DeletionProtection }},
		{"--deletion-protection=false turns protection off", []string{"--deletion-protection=false"}, func(db *client.Database) bool {
			return !db.DeletionProtection && !db.HighAvailability
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			f := factoryWith(t, fake)
			if _, _, err := run(f, newUpdateCmd(f), append([]string{"orders-db"}, tt.args...)...); err != nil {
				t.Fatalf("update: %v", err)
			}
			if db := fake.Databases["orders-db"]; !tt.check(db) {
				t.Fatalf("after update: %+v resources %+v", db, db.Resources)
			}
		})
	}
}

func TestUpdate_Refusals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no flags is misuse", []string{"orders-db"}, cli.ExitMisuse},
		{"malformed memory is misuse", []string{"orders-db", "--memory", "2"}, cli.ExitMisuse},
		{"missing database exits 4", []string{"ghost", "--ha"}, cli.ExitNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			f := factoryWith(t, fake)
			_, _, err := run(f, newUpdateCmd(f), tt.args...)
			assertExit(t, err, tt.want)
		})
	}
}

func TestUpdate_ShowsAPIFieldReasons(t *testing.T) {
	fake := clienttest.New()
	fake.Databases["orders-db"] = running("orders-db")
	fake.UpdateDatabaseErr = &client.APIError{StatusCode: 400,
		Message: `invalid database "orders-db": spec.resources.storage: storage can only grow`}
	f := factoryWith(t, fake)
	_, _, err := run(f, newUpdateCmd(f), "orders-db", "--storage", "5Gi")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(err.Error(), "spec.resources.storage: storage can only grow") {
		t.Fatalf("field reason should be shown: %v", err)
	}
}

func TestUpdate_StorageShrinkIsMisuseWithAHint(t *testing.T) {
	fake := clienttest.New()
	fake.Databases["orders-db"] = running("orders-db")
	fake.UpdateDatabaseErr = &client.APIError{StatusCode: 400, Code: client.DatabaseCodeStorageShrinkUnsupported,
		Message: "storage can only grow: 5Gi is smaller than the current 10Gi"}
	f := factoryWith(t, fake)
	_, _, err := run(f, newUpdateCmd(f), "orders-db", "--storage", "5Gi")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(err.Error(), "storage can grow but never shrink") || !strings.Contains(err.Error(), "smaller than the current 10Gi") {
		t.Fatalf("should explain and keep the API text: %v", err)
	}
	if !strings.Contains(hint(err), "kupe db get orders-db") {
		t.Fatalf("hint = %q; want the get command", hint(err))
	}
}

func TestUpgrade(t *testing.T) {
	refused := &client.APIError{StatusCode: 400, Code: client.DatabaseCodeMajorUpgradeUnavailable, Message: "changing the major version is not supported yet"}
	tests := []struct {
		name        string
		version     string
		updateErr   error
		wantExit    int
		wantVersion string
		wantErrText []string
	}{
		{"sets the new major", "19", nil, cli.ExitOK, "19", nil},
		{"same major is a no-op", "18", nil, cli.ExitOK, "18", nil},
		{"rejects a non-major version", "18.1", nil, cli.ExitMisuse, "18", []string{"major version"}},
		{"a platform refusal exits 7 with the API's reason", "19", refused, cli.ExitUnavailable, "18",
			[]string{"the platform refused the major version upgrade: changing the major version is not supported yet"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			fake.UpdateDatabaseErr = tt.updateErr
			f := factoryWith(t, fake)
			_, stderr, err := run(f, newUpgradeCmd(f), "orders-db", "--version", tt.version)
			assertExit(t, err, tt.wantExit)
			if got := fake.Databases["orders-db"].Version; got != tt.wantVersion {
				t.Fatalf("version = %q; want %q", got, tt.wantVersion)
			}
			for _, w := range tt.wantErrText {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
			if stderr != "" {
				t.Errorf("stderr should be empty; the platform's refusal is the only notice:\n%s", stderr)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name      string
		protected bool
		args      []string
		answer    string // non-empty → interactive with this stdin
		deleteErr error
		wantExit  int
		wantGone  bool
		wantHint  string
	}{
		{"--yes deletes an unprotected database", false, []string{"--yes"}, "", nil, cli.ExitOK, true, ""},
		{"typing the name confirms", false, nil, "orders-db\n", nil, cli.ExitOK, true, ""},
		{"typing yes is not enough", false, nil, "yes\n", nil, cli.ExitMisuse, false, ""},
		{"non-interactive without --yes is refused", false, nil, "", nil, cli.ExitMisuse, false, ""},
		{"deletion protection exits 5 with the update hint", true, []string{"--yes"}, "", nil, cli.ExitConflict, false,
			"kupe db update orders-db --deletion-protection=false"},
		{"a concurrent change exits 5 and suggests a retry", false, []string{"--yes"}, "",
			&client.APIError{StatusCode: 409, Message: "the object has been modified"}, cli.ExitConflict, false, "retry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			db := running("orders-db")
			db.DeletionProtection = tt.protected
			fake.Databases["orders-db"] = db
			fake.DeleteDatabaseErr = tt.deleteErr
			f := factoryWith(t, fake)
			if tt.answer != "" {
				interactive(f, tt.answer)
			}
			out, _, err := run(f, newDeleteCmd(f), append([]string{"orders-db"}, tt.args...)...)
			assertExit(t, err, tt.wantExit)
			if _, still := fake.Databases["orders-db"]; still == tt.wantGone {
				t.Fatalf("database present=%v; want gone=%v", still, tt.wantGone)
			}
			if tt.wantGone && out != "database/orders-db delete requested\n" {
				t.Fatalf("stdout = %q", out)
			}
			if tt.wantHint != "" && !strings.Contains(hint(err), tt.wantHint) {
				t.Fatalf("hint = %q; want containing %q", hint(err), tt.wantHint)
			}
		})
	}
}

func TestDelete_StatesRetentionVerbatimInHelpAndWarning(t *testing.T) {
	const notice = "Deleting a database removes it immediately. Its storage is kept for 7 days for support-assisted recovery, then permanently deleted. There is no self-service restore in v1."
	fake := clienttest.New()
	db := running("orders-db")
	db.DeletionProtection = false
	fake.Databases["orders-db"] = db
	f := factoryWith(t, fake)
	interactive(f, "orders-db\n")
	cmd := newDeleteCmd(f)
	if !strings.Contains(cmd.Long, notice) {
		t.Errorf("help is missing the retention notice:\n%s", cmd.Long)
	}
	_, stderr, err := run(f, cmd, "orders-db")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(stderr, notice) {
		t.Errorf("warning is missing the retention notice:\n%s", stderr)
	}
}

func TestRotateCredentials(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		answer    string
		rotateErr error
		wantExit  int
		wantCall  string
	}{
		{"--yes rotates the app role", []string{"--role", "app", "--yes"}, "", nil, cli.ExitOK, "RotateDatabaseCredentials:orders-db:app"},
		{"typing yes confirms the owner role", []string{"--role", "owner"}, "yes\n", nil, cli.ExitOK, "RotateDatabaseCredentials:orders-db:owner"},
		{"role is required", []string{"--yes"}, "", nil, cli.ExitMisuse, ""},
		{"unknown role is misuse", []string{"--role", "admin", "--yes"}, "", nil, cli.ExitMisuse, ""},
		{"non-interactive without --yes is refused", []string{"--role", "app"}, "", nil, cli.ExitMisuse, ""},
		{"a database being deleted exits 5", []string{"--role", "app", "--yes"}, "",
			&client.APIError{StatusCode: 409, Message: "database is being deleted"}, cli.ExitConflict, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			fake.RotateCredentialsErr = tt.rotateErr
			f := factoryWith(t, fake)
			if tt.answer != "" {
				interactive(f, tt.answer)
			}
			out, stderr, err := run(f, newRotateCredentialsCmd(f), append([]string{"orders-db"}, tt.args...)...)
			assertExit(t, err, tt.wantExit)
			if tt.wantCall == "" {
				if tt.rotateErr == nil {
					assertNoCall(t, fake, "RotateDatabaseCredentials")
				}
				return
			}
			if fake.Calls[len(fake.Calls)-1] != tt.wantCall {
				t.Fatalf("calls = %v; want last %q", fake.Calls, tt.wantCall)
			}
			if !strings.Contains(stderr, "must restart") {
				t.Errorf("stderr should warn that applications must restart:\n%s", stderr)
			}
			if !strings.Contains(out, "rotation requested") {
				t.Errorf("stdout = %q", out)
			}
		})
	}
}

func TestWait(t *testing.T) {
	tests := []struct {
		name    string
		seed    bool
		forFlag string
		wantOut string
	}{
		{"running database", true, "running", "database/orders-db running\n"},
		{"missing database counts as deleted", false, "deleted", "database/orders-db deleted\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			if tt.seed {
				fake.Databases["orders-db"] = running("orders-db")
			}
			f := factoryWith(t, fake)
			out, _, err := run(f, newWaitCmd(f), "orders-db", "--for", tt.forFlag, "--timeout", "5s")
			if err != nil {
				t.Fatalf("wait: %v", err)
			}
			if out != tt.wantOut {
				t.Fatalf("stdout = %q; want %q", out, tt.wantOut)
			}
		})
	}
}

func TestWait_ForDeletedWhenDatabasesUnavailableIsAnError(t *testing.T) {
	fake := clienttest.New()
	fake.GetDatabaseErr = &client.APIError{StatusCode: 404, Code: client.DatabaseCodeUnavailable, Message: "managed databases are not available on this platform"}
	f := factoryWith(t, fake)
	out, _, err := run(f, newWaitCmd(f), "orders-db", "--for", "deleted", "--timeout", "5s")
	assertExit(t, err, cli.ExitNotFound)
	if out != "" {
		t.Fatalf("must not report the database deleted: %q", out)
	}
}

func TestBindingAdd(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantName   string
		wantSecret string
	}{
		{"defaults the name from database, cluster, namespace and role", []string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "app"},
			cli.ExitOK, "orders-db-prod-api-app", ""},
		{"explicit name and secret name", []string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "owner", "--name", "migrate", "--secret-name", "pg-owner"},
			cli.ExitOK, "migrate", "pg-owner"},
		{"a default name over 63 characters asks for --name", []string{"orders-db", "--cluster", strings.Repeat("c", 50), "--namespace", "api", "--role", "app"},
			cli.ExitMisuse, "", ""},
		{"cluster is required", []string{"orders-db", "--namespace", "api", "--role", "app"}, cli.ExitMisuse, "", ""},
		{"role must be owner or app", []string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "admin"}, cli.ExitMisuse, "", ""},
		{"missing database exits 4", []string{"ghost", "--cluster", "prod", "--namespace", "api", "--role", "app"}, cli.ExitNotFound, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			f := factoryWith(t, fake)
			// --wait=false: these cases check the request; TestBindingAdd_Wait
			// covers the wait. The short timeout bounds the test if
			// --wait=false were ignored.
			_, _, err := run(f, newBindingAddCmd(f), append(tt.args, "--wait=false", "--wait-timeout", "50ms")...)
			assertExit(t, err, tt.wantExit)
			if tt.wantName == "" {
				return
			}
			b := fake.DatabaseBindings[tt.wantName]
			if b == nil {
				t.Fatalf("binding %q not created; have %v", tt.wantName, fake.DatabaseBindings)
			}
			if b.Database != "orders-db" || b.Cluster != "prod" || b.Namespace != "api" || b.SecretName != tt.wantSecret {
				t.Fatalf("binding = %+v", b)
			}
		})
	}
}

// boundAs is the binding `binding add orders-db --cluster prod --namespace
// api --role app` creates, as the API reports it in phase.
func boundAs(phase, message string, conditions ...client.Condition) *client.DatabaseBinding {
	return &client.DatabaseBinding{
		Name: "orders-db-prod-api-app", Database: "orders-db", Cluster: "prod", Namespace: "api", Role: "app",
		Status: &client.DatabaseBindingStatus{
			Phase: phase, SecretName: "orders-db-app", Message: message, Conditions: conditions,
		},
	}
}

func TestBindingAdd_Wait(t *testing.T) {
	const conflictMessage = "the namespace already has a Service or Secret of the binding's name that the platform " +
		"did not create; remove it, or bind with another secretName"
	conflict := []client.Condition{
		{Type: "Ready", Status: "False", Reason: "SyncConflict", Message: conflictMessage},
		{Type: "Conflict", Status: "True", Reason: "SyncConflict", Message: conflictMessage},
	}
	check := "kupe database binding get orders-db orders-db-prod-api-app"
	tests := []struct {
		name     string
		seq      []*client.DatabaseBinding
		args     []string
		wantExit int
		wantGets int
		wantOut  string
		wantErr  []string
		wantHint []string
		noHint   string
	}{
		{
			name: "ready renders the bound binding", seq: []*client.DatabaseBinding{boundAs("Ready", "the database is available in the namespace")},
			args: []string{"--wait-timeout", "5s"}, wantExit: cli.ExitOK, wantGets: 1, wantOut: "Ready",
		},
		{
			name: "a name conflict fails at once with what to change",
			seq:  []*client.DatabaseBinding{boundAs("Failed", conflictMessage, conflict...)},
			args: []string{"--wait-timeout", "5s"}, wantExit: cli.ExitConflict, wantGets: 1,
			wantErr: []string{`binding "orders-db-prod-api-app" failed: ` + conflictMessage},
			wantHint: []string{
				`namespace api of cluster prod already has a Service "orders-db" or a Secret "orders-db-app" that the platform did not create`,
				"rename or remove that object",
				"kupe database binding delete orders-db orders-db-prod-api-app --yes",
				"--secret-name",
				check,
			},
		},
		{
			name:     "a failure without a conflict gives no conflict advice",
			seq:      []*client.DatabaseBinding{boundAs("Failed", "something else went wrong")},
			args:     []string{"--wait-timeout", "5s"},
			wantExit: cli.ExitGeneral, wantGets: 1,
			wantErr:  []string{"something else went wrong"},
			wantHint: []string{check},
			noHint:   "rename or remove",
		},
		{
			name: "pending past the timeout shows why and how to check",
			seq:  []*client.DatabaseBinding{boundAs("Pending", "cluster prod does not exist")},
			args: []string{"--wait-timeout", "50ms"}, wantExit: cli.ExitTimeout, wantGets: 1,
			wantErr:  []string{`binding "orders-db-prod-api-app" is still Pending: cluster prod does not exist`},
			wantHint: []string{"becomes Ready on its own", check},
		},
		{
			name: "--wait=false returns the accepted binding without polling",
			// The short timeout bounds the test if --wait=false were ignored.
			args: []string{"--wait=false", "--wait-timeout", "50ms"}, wantExit: cli.ExitOK, wantGets: 0, wantOut: "Pending",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Databases["orders-db"] = running("orders-db")
			fake.GetDatabaseBindingSeq["orders-db-prod-api-app"] = tt.seq
			f := factoryWith(t, fake)
			args := append([]string{"orders-db", "--cluster", "prod", "--namespace", "api", "--role", "app"}, tt.args...)
			out, _, err := run(f, newBindingAddCmd(f), args...)
			assertExit(t, err, tt.wantExit)
			gets := 0
			for _, c := range fake.Calls {
				if strings.HasPrefix(c, "GetDatabaseBinding:") {
					gets++
				}
			}
			if gets != tt.wantGets {
				t.Errorf("GetDatabaseBinding calls = %d; want %d", gets, tt.wantGets)
			}
			if tt.wantOut != "" && !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout should contain %q:\n%s", tt.wantOut, out)
			}
			for _, want := range tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("error should contain %q: %v", want, err)
				}
			}
			for _, want := range tt.wantHint {
				if !strings.Contains(hint(err), want) {
					t.Errorf("hint should contain %q:\n%s", want, hint(err))
				}
			}
			if tt.noHint != "" && strings.Contains(hint(err), tt.noHint) {
				t.Errorf("hint should not contain %q:\n%s", tt.noHint, hint(err))
			}
		})
	}
}

func TestBindingListGetDelete(t *testing.T) {
	seed := func() *clienttest.Fake {
		fake := clienttest.New()
		fake.Databases["orders-db"] = running("orders-db")
		fake.Databases["users-db"] = running("users-db")
		fake.DatabaseBindings["orders-db-prod-api-app"] = &client.DatabaseBinding{
			Name: "orders-db-prod-api-app", Database: "orders-db", Cluster: "prod", Namespace: "api", Role: "app",
			Status: &client.DatabaseBindingStatus{Phase: "Ready", Host: "orders-db.api.svc.cluster.local", SecretName: "orders-db-app"},
		}
		fake.DatabaseBindings["users-db-prod-web-app"] = &client.DatabaseBinding{Name: "users-db-prod-web-app", Database: "users-db"}
		return fake
	}

	t.Run("list shows only that database's bindings", func(t *testing.T) {
		f := factoryWith(t, seed())
		out, _, err := run(f, newBindingListCmd(f), "orders-db", "-o", "wide")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if !strings.Contains(out, "orders-db-prod-api-app") || !strings.Contains(out, "orders-db-app") ||
			!strings.Contains(out, "orders-db.api.svc.cluster.local") || strings.Contains(out, "users-db") {
			t.Fatalf("unexpected list:\n%s", out)
		}
	})
	t.Run("get shows host and secret", func(t *testing.T) {
		f := factoryWith(t, seed())
		out, _, err := run(f, newBindingGetCmd(f), "orders-db", "orders-db-prod-api-app")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !strings.Contains(out, "orders-db.api.svc.cluster.local") || !strings.Contains(out, "orders-db-app") {
			t.Fatalf("unexpected details:\n%s", out)
		}
	})
	t.Run("get of another database's binding exits 4", func(t *testing.T) {
		f := factoryWith(t, seed())
		_, _, err := run(f, newBindingGetCmd(f), "orders-db", "users-db-prod-web-app")
		assertExit(t, err, cli.ExitNotFound)
	})
	t.Run("delete without --yes is refused non-interactively", func(t *testing.T) {
		fake := seed()
		f := factoryWith(t, fake)
		_, _, err := run(f, newBindingDeleteCmd(f), "orders-db", "orders-db-prod-api-app")
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "DeleteDatabaseBinding")
	})
	t.Run("delete is requested and points at rotation", func(t *testing.T) {
		fake := seed()
		f := factoryWith(t, fake)
		out, stderr, err := run(f, newBindingDeleteCmd(f), "orders-db", "orders-db-prod-api-app", "--yes")
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, still := fake.DatabaseBindings["orders-db-prod-api-app"]; still {
			t.Fatal("binding still present")
		}
		if out != "binding/orders-db-prod-api-app delete requested\n" || !strings.Contains(stderr, "rotate-credentials orders-db") {
			t.Fatalf("stdout=%q stderr=%q", out, stderr)
		}
	})
}

func TestNewCmd_RegistersAliasAndSubcommands(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	cmd := NewCmd(f)
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "db" {
		t.Fatalf("aliases = %v; want [db]", cmd.Aliases)
	}
	for _, path := range [][]string{
		{"list"}, {"get"}, {"create"}, {"update"}, {"upgrade"}, {"delete"}, {"rotate-credentials"}, {"wait"},
		{"binding", "add"}, {"binding", "list"}, {"binding", "get"}, {"binding", "delete"},
	} {
		if found, _, err := cmd.Find(path); err != nil || found == cmd {
			t.Errorf("subcommand %v not registered", path)
		}
	}
}
