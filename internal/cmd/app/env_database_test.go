package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestParseDatabaseFlag(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want client.AppDatabase
	}{
		{"main=orders", client.AppDatabase{Name: "main", Database: "orders"}},
		{"main=orders/shop", client.AppDatabase{Name: "main", Database: "orders", LogicalDatabase: "shop"}},
		{"reports=orders:REPORTS_URL", client.AppDatabase{Name: "reports", Database: "orders", Env: "REPORTS_URL"}},
		{"reports=orders/analytics:REPORTS_URL", client.AppDatabase{Name: "reports", Database: "orders", LogicalDatabase: "analytics", Env: "REPORTS_URL"}},
	} {
		got, err := parseDatabaseFlag(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v err=%v", tc.in, got, err)
		}
	}
	for _, in := range []string{"", "main", "=orders", "main=", "main=/shop", "main=orders/", "main=orders:", "main=:URL"} {
		_, err := parseDatabaseFlag(in)
		if cli.ExitCode(err) != 2 || err.Error() != "--database must use CONNECTION=DATABASE[/POSTGRES_DATABASE][:VARIABLE]" {
			t.Errorf("%q: err=%v", in, err)
		}
	}
}

func envWithDatabases(dbs ...client.AppDatabase) *client.AppEnvironment {
	e := seededEnv()
	e.Databases = dbs
	return e
}

func updateDatabases(t *testing.T, env *client.AppEnvironment, args ...string) (*clienttest.Fake, error) {
	t.Helper()
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": env}
	_, _, err := run(factoryWith(fake), append([]string{"env", "update", "shop", "production", "--wait=false"}, args...)...)
	return fake, err
}

func TestEnvironmentDatabaseFlagsMerge(t *testing.T) {
	main := client.AppDatabase{Name: "main", Database: "orders", Env: "DATABASE_URL"}
	for _, tc := range []struct {
		name   string
		before []client.AppDatabase
		args   []string
		want   []client.AppDatabase
	}{
		{"add with default variable", nil, []string{"--database", "main=orders"}, []client.AppDatabase{main}},
		{"add with logical database and variable", []client.AppDatabase{main}, []string{"--database", "reports=orders/analytics:REPORTS_URL"},
			[]client.AppDatabase{main, {Name: "reports", Database: "orders", LogicalDatabase: "analytics", Env: "REPORTS_URL"}}},
		{"same database changes only the variable", []client.AppDatabase{main}, []string{"--database", "main=orders:ORDERS_URL"},
			[]client.AppDatabase{{Name: "main", Database: "orders", Env: "ORDERS_URL"}}},
		{"another connection keeps the first one's variable", []client.AppDatabase{{Name: "main", Database: "orders", Env: "ORDERS_URL"}}, []string{"--database", "main=orders", "--database", "other=billing:B_URL"},
			[]client.AppDatabase{{Name: "main", Database: "orders", Env: "ORDERS_URL"}, {Name: "other", Database: "billing", Env: "B_URL"}}},
		{"remove", []client.AppDatabase{main, {Name: "reports", Database: "orders", LogicalDatabase: "analytics", Env: "R"}}, []string{"--remove-database", "reports"}, []client.AppDatabase{main}},
		{"remove the last sends an empty list", []client.AppDatabase{main}, []string{"--remove-database", "main"}, []client.AppDatabase{}},
		{"add and remove different connections", []client.AppDatabase{main}, []string{"--database", "other=billing:B_URL", "--remove-database", "main"}, []client.AppDatabase{{Name: "other", Database: "billing", Env: "B_URL"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := updateDatabases(t, envWithDatabases(tc.before...), tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(fake.AppEnvironmentPatches) != 1 || fake.AppEnvironmentPatches[0].Databases == nil {
				t.Fatalf("patches=%+v", fake.AppEnvironmentPatches)
			}
			got, _ := json.Marshal(*fake.AppEnvironmentPatches[0].Databases)
			want, _ := json.Marshal(tc.want)
			if string(got) != string(want) {
				t.Fatalf("got %s want %s", got, want)
			}
		})
	}
}

func TestEnvironmentPatchCarriesDatabasesOnlyWhenChanged(t *testing.T) {
	main := client.AppDatabase{Name: "main", Database: "orders", Env: "DATABASE_URL"}
	for _, tc := range []struct {
		name   string
		before []client.AppDatabase
		args   []string
		want   bool
	}{
		{"unrelated edit", []client.AppDatabase{main}, []string{"--port", "9090"}, false},
		{"identical connection", []client.AppDatabase{main}, []string{"--database", "main=orders", "--port", "9090"}, false},
		{"unknown removal is a no-op", nil, []string{"--remove-database", "ghost", "--port", "9090"}, false},
		{"change", []client.AppDatabase{main}, []string{"--database", "main=orders:X"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := updateDatabases(t, envWithDatabases(tc.before...), tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(fake.AppEnvironmentPatches[0])
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(data), `"databases"`); has != tc.want {
				t.Fatalf("databases in patch=%v: %s", has, data)
			}
		})
	}
	fake, err := updateDatabases(t, envWithDatabases(main), "--remove-database", "main")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(fake.AppEnvironmentPatches[0])
	if string(data) != `{"databases":[]}` {
		t.Fatalf("removing the last connection: %s", data)
	}
}

func TestEnvironmentDatabaseNothingChangedSendsNoPatch(t *testing.T) {
	fake, err := updateDatabases(t, envWithDatabases(client.AppDatabase{Name: "main", Database: "orders", Env: "DATABASE_URL"}), "--database", "main=orders")
	if err != nil || len(fake.AppEnvironmentPatches) != 0 {
		t.Fatalf("err=%v patches=%+v", err, fake.AppEnvironmentPatches)
	}
}

func TestEnvironmentDatabaseMisuse(t *testing.T) {
	main := client.AppDatabase{Name: "main", Database: "orders", Env: "DATABASE_URL"}
	for _, tc := range []struct {
		name   string
		before []client.AppDatabase
		args   []string
		want   string
		hint   string
	}{
		{"bad form", nil, []string{"--database", "main"}, "--database must use CONNECTION=DATABASE[/POSTGRES_DATABASE][:VARIABLE]", ""},
		{"different database", []client.AppDatabase{main}, []string{"--database", "main=billing"},
			`connection "main" already uses database "orders"; remove it with --remove-database main, then add it again in a second update`, ""},
		{"different logical database", []client.AppDatabase{main}, []string{"--database", "main=orders/shop"},
			`connection "main" already uses database "orders"; remove it with --remove-database main, then add it again in a second update`, ""},
		{"add and remove the same", []client.AppDatabase{main}, []string{"--database", "main=orders:X", "--remove-database", "main"},
			`--database and --remove-database both name connection "main"; run two updates`, ""},
		{"more than five", nil, []string{"--database", "a=d1:A", "--database", "b=d2:B", "--database", "c=d3:C", "--database", "d=d4:D", "--database", "e=d5:E", "--database", "f=d6:F"},
			"databases must contain at most 5 connections", ""},
		{"bad name", nil, []string{"--database", "Bad=orders"}, "connection names must be unique RFC 1123 labels of at most 55 characters", ""},
		{"long name", nil, []string{"--database", strings.Repeat("a", 56) + "=orders"}, "connection names must be unique RFC 1123 labels of at most 55 characters", ""},
		{"bad logical database", nil, []string{"--database", "main=orders/Shop"},
			"logicalDatabase must start with a lowercase letter and use only lowercase letters, digits and underscores, at most 53 characters", ""},
		{"bad variable", nil, []string{"--database", "main=orders:9BAD"}, "env must be a valid environment variable name", ""},
		{"web env collision", nil, []string{"--database", "main=orders:A"},
			`variable "A" is already set in web.env; choose another name with env`, "set the variable with --database CONNECTION=DATABASE:VARIABLE"},
		{"default variable collides with another connection", nil, []string{"--database", "main=orders", "--database", "reports=billing"},
			`variable "DATABASE_URL" is already used by connection "main"; choose another name with env`, "set the variable with --database CONNECTION=DATABASE:VARIABLE"},
		{"same database twice", nil, []string{"--database", "main=orders:A1", "--database", "reports=orders:R1"},
			`connections "main" and "reports" both use database "orders"; remove one of them`, ""},
		{"same logical database twice", nil, []string{"--database", "main=orders/shop:A1", "--database", "reports=orders/shop:R1"},
			`connections "main" and "reports" both use logical database "shop" of database "orders"; remove one of them`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := updateDatabases(t, envWithDatabases(tc.before...), tc.args...)
			var ce *cli.Error
			if cli.ExitCode(err) != 2 || err.Error() != tc.want || len(fake.AppEnvironmentPatches) != 0 {
				t.Fatalf("exit=%d err=%v patches=%v", cli.ExitCode(err), err, fake.AppEnvironmentPatches)
			}
			if tc.hint != "" && (!errors.As(err, &ce) || ce.Hint != tc.hint) {
				t.Fatalf("hint=%v", err)
			}
		})
	}
}

func TestEnvironmentCreateWithDatabasesAndFile(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(fake)
	f.IOStreams.In = strings.NewReader(`{"databases":[{"name":"main","database":"orders"},{"name":"reports","database":"orders","logicalDatabase":"analytics","env":"REPORTS_URL"}]}`)
	_, _, err := run(f, "env", "create", "shop", "production", "-f", "-", "--cluster", "europe", "--image", digest, "--port", "8080", "--database", "extra=billing:BILLING_URL", "--wait=false")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(fake.AppEnvironmentCreates[0].Databases)
	want := `[{"name":"main","database":"orders","env":"DATABASE_URL"},{"name":"reports","database":"orders","logicalDatabase":"analytics","env":"REPORTS_URL"},{"name":"extra","database":"billing","env":"BILLING_URL"}]`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
}

func TestEnvironmentFileDatabasesReplaceTheList(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"replace", `{"databases":[{"name":"new","database":"billing","env":"B_URL"}]}`, `[{"name":"new","database":"billing","env":"B_URL"}]`},
		{"empty", `{"databases":[]}`, `[]`},
		{"null", `{"databases":null}`, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": envWithDatabases(client.AppDatabase{Name: "main", Database: "orders", Env: "DATABASE_URL"})}
			f := factoryWith(fake)
			f.IOStreams.In = strings.NewReader(tc.input)
			_, _, err := run(f, "env", "update", "shop", "production", "-f", "-", "--wait=false")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(fake.AppEnvironmentPatches[0])
			if string(data) != `{"databases":`+tc.want+`}` {
				t.Fatalf("patch=%s", data)
			}
		})
	}
	fake := clienttest.New()
	f := factoryWith(fake)
	f.IOStreams.In = strings.NewReader(`{"databases":[{"name":"main","database":"orders","surprise":1}]}`)
	_, _, err := run(f, "env", "create", "shop", "production", "-f", "-", "--cluster", "europe", "--image", digest, "--port", "8080")
	if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), `unknown field "surprise"`) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnvironmentRMWRefusesConcurrentChangeToFlaggedConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		changed bool
	}{
		{"flagged connection changed", []string{"--database", "main=orders:NEW_URL"}, true},
		{"removed connection changed", []string{"--remove-database", "main"}, true},
		{"unflagged connection changed", []string{"--database", "other=billing:B_URL"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := clienttest.New()
			first := envWithDatabases(client.AppDatabase{Name: "main", Database: "orders", Env: "DATABASE_URL"})
			next := envWithDatabases(client.AppDatabase{Name: "main", Database: "orders", Env: "CHANGED_BY_OTHER"})
			next.Generation = 2
			fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": next}
			fake.GetAppEnvironmentSeq["shop"] = map[string][]*client.AppEnvironment{"production": {first, next}}
			fake.UpdateAppEnvironmentErrors = []error{&client.APIError{StatusCode: 412, Code: "precondition_failed"}, nil}
			_, _, err := run(factoryWith(fake), append([]string{"env", "update", "shop", "production", "--wait=false"}, tc.args...)...)
			if tc.changed {
				if cli.ExitCode(err) != 5 || len(fake.AppEnvironmentPatches) != 0 {
					t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
				}
				return
			}
			if err != nil || len(fake.AppEnvironmentPatches) != 1 {
				t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
			}
		})
	}
}

func TestEnvironmentDatabaseErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		code      string
		exit      int
		hint, msg string
	}{
		{"APP_DATABASE_DUPLICATE", 2, "connect each database once per environment; remove the extra connection with --remove-database NAME", "connections main and reports both use database orders"},
		{"APP_DATABASE_ENV_CONFLICT", 2, "", `variable "DATABASE_URL" is already set in web.env`},
		{"APP_DATABASE_SECRET_IN_USE", 2, "", `secret "main-app" is reserved`},
		{"databases_unavailable", 4, "-", "managed databases are not enabled on this platform"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			fake := clienttest.New()
			status := 400
			if tc.exit == 4 {
				status = 404
			}
			fake.CreateAppEnvironmentErr = &client.APIError{StatusCode: status, Code: tc.code, Message: tc.msg}
			_, _, err := run(factoryWith(fake), "env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080", "--database", "main=orders", "--wait=false")
			var ce *cli.Error
			hint := ""
			if errors.As(err, &ce) {
				hint = ce.Hint
			}
			if cli.ExitCode(err) != tc.exit || !strings.Contains(err.Error(), tc.msg) || (tc.hint != "-" && hint != tc.hint) {
				t.Fatalf("exit=%d err=%v hint=%q", cli.ExitCode(err), err, hint)
			}
		})
	}
}

func TestEnvironmentDeleteCopyKeepsDatabases(t *testing.T) {
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": seededEnv()}
	// Without --yes the warning prints before the confirmation, which a
	// non-interactive run then refuses.
	_, stderr, _ := run(factoryWith(fake), "env", "delete", "shop", "production")
	if !strings.Contains(stderr, "The cluster, Vault entries and connected databases and their backups are kept.") {
		t.Fatalf("warning missing: %q", stderr)
	}
	help, _, err := run(factoryWith(fake), "env", "delete", "--help")
	if err != nil || !strings.Contains(strings.Join(strings.Fields(help), " "), "The cluster, Vault entries and connected databases are kept; only this environment's database connections are removed.") {
		t.Fatalf("err=%v help=%q", err, help)
	}
}

func TestEnvironmentNamespaceInUseHint(t *testing.T) {
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": seededEnv()}
	fake.DeleteAppEnvironmentErr = &client.APIError{StatusCode: 409, Code: "namespace_in_use", Message: "namespace in use"}
	_, _, err := run(factoryWith(fake), "env", "delete", "shop", "production", "--yes")
	var ce *cli.Error
	if cli.ExitCode(err) != 5 || !errors.As(err, &ce) || ce.Hint != "remove secrets and database bindings you created yourself in the namespace, then delete again; the environment's own database connections don't block it" {
		t.Fatalf("err=%v", err)
	}
}
