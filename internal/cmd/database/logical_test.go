package database

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func logicalDB(name string, entries ...client.LogicalDatabase) *client.Database {
	db := running(name)
	db.Postgres = &client.DatabasePostgres{Databases: &entries}
	for _, entry := range entries {
		db.Status.Databases = append(db.Status.Databases, client.LogicalDatabaseStatus{Name: entry.Name, Phase: client.PhaseReady})
	}
	return db
}
func entry(name string, schemas ...string) client.LogicalDatabase {
	out := client.LogicalDatabase{Name: name, Schemas: []client.DatabaseSchema{}}
	for _, name := range schemas {
		out.Schemas = append(out.Schemas, client.DatabaseSchema{Name: name})
	}
	return out
}

func TestCreate_LogicalDatabasesAndSchemas(t *testing.T) {
	for _, name := range []string{"orders-db", "pg-x"} {
		t.Run(name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			out, _, err := run(f, newCreateCmd(f), name, "--cpu", "500m", "--memory", "1Gi", "--storage", "5Gi", "--logical-database", "analytics", "--schema", "reporting", "--schema", "analytics.rollups", "--roles", "auditor", "--extensions", "pg_trgm", "--wait=false", "-o", "json")
			if err != nil {
				t.Fatal(err)
			}
			var db client.Database
			if err := json.Unmarshal([]byte(out), &db); err != nil {
				t.Fatal(err)
			}
			want := []client.LogicalDatabase{entry(strings.ReplaceAll(name, "-", "_"), "reporting"), entry("analytics", "rollups")}
			if db.Postgres == nil || db.Postgres.Databases == nil || !reflect.DeepEqual(*db.Postgres.Databases, want) {
				t.Fatalf("postgres = %+v", db.Postgres)
			}
			if db.Postgres.Roles == nil || db.Postgres.Extensions == nil {
				t.Fatalf("lost role/extension flags: %+v", db.Postgres)
			}
			var wire map[string]any
			if err := json.Unmarshal([]byte(out), &wire); err != nil {
				t.Fatal(err)
			}
			if _, ok := wire["databases"]; ok {
				t.Fatal("top-level databases")
			}
		})
	}
}

func TestCreate_LogicalValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--logical-database", strings.Repeat("a", 54)}, {"--logical-database", "samegroup"}, {"--logical-database", "app"}, {"--logical-database", "pg"}, {"--logical-database", "mdbl"}, {"--logical-database", "mdbl_reports"}, {"--logical-database", "CNPG"},
		{"--schema", "public"}, {"--schema", "information_schema"}, {"--schema", "pg_private"}, {"--schema", strings.Repeat("a", 64)}, {"--schema", "a.b.c"}, {"--schema", "Analytics.reporting"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			_, _, err := run(f, newCreateCmd(f), append([]string{"orders", "--cpu", "500m", "--memory", "1Gi", "--storage", "5Gi", "--wait=false"}, args...)...)
			assertExit(t, err, cli.ExitMisuse)
			if len(fake.Calls) > 0 {
				t.Fatalf("calls = %v", fake.Calls)
			}
		})
	}
}

func TestCreate_LogicalNamesFirstAndCaps(t *testing.T) {
	for _, first := range []string{"app", "pg_x", "mdbl_first"} {
		list, _, err := appendLogical([]client.LogicalDatabase{entry(first)}, []string{first}, nil)
		if err != nil || len(list) != 1 {
			t.Fatalf("first %s: %v, %v", first, list, err)
		}
	}
	names := []string{}
	for i := 0; i < 9; i++ {
		names = append(names, fmt.Sprintf("d%d", i))
	}
	list, _, err := appendLogical([]client.LogicalDatabase{entry("orders")}, names, nil)
	if err != nil || len(list) != 10 {
		t.Fatalf("10 boundary %v %v", list, err)
	}
	_, _, err = appendLogical(list, []string{"overflow"}, nil)
	assertExit(t, err, cli.ExitMisuse)
	schemas := []string{}
	for i := 0; i < 20; i++ {
		schemas = append(schemas, fmt.Sprintf("s%d", i))
	}
	list, _, err = appendLogical([]client.LogicalDatabase{entry("orders")}, nil, schemas)
	if err != nil || len(list[0].Schemas) != 20 {
		t.Fatalf("20 boundary %v %v", list, err)
	}
	_, _, err = appendLogical(list, nil, []string{"overflow"})
	assertExit(t, err, cli.ExitMisuse)
	_, _, err = appendLogical([]client.LogicalDatabase{entry("orders")}, []string{strings.Repeat("a", 53)}, []string{strings.Repeat("s", 63)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreate_RestoreLogicalFlagsRefusedBeforeHTTP(t *testing.T) {
	for _, source := range []string{"--from", "--from-backup"} {
		for _, setting := range []string{"--logical-database", "--schema"} {
			t.Run(source+setting, func(t *testing.T) {
				fake := clienttest.New()
				f := factoryWith(t, fake)
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unexpected", 500) }))
				defer srv.Close()
				f.Client = func() (client.Interface, error) {
					t.Error("client resolved before local refusal")
					return client.New(srv.URL, "acme", "synthetic", "test"), nil
				}
				_, _, err := run(f, newCreateCmd(f), "restored", "--cpu", "500m", "--memory", "1Gi", source, "original", setting, "analytics", "--wait=false")
				assertExit(t, err, cli.ExitMisuse)
				if calls != 0 {
					t.Fatalf("requests = %d", calls)
				}
				if !strings.Contains(hint(err), "restore first") {
					t.Fatalf("hint = %s", hint(err))
				}
			})
		}
	}
}

func TestUpdate_AddLogicalDatabaseAppends(t *testing.T) {
	fake := clienttest.New()
	db := logicalDB("restored", entry("source_first", "keep"), entry("warehouse", "raw"))
	roles := []client.DeclaredRole{{Name: "auditor"}}
	extensions := []client.Extension{{Name: "pg_trgm"}}
	db.Postgres.Roles = &roles
	db.Postgres.Extensions = &extensions
	fake.DatabasesByName[db.Name] = db
	f := factoryWith(t, fake)
	_, _, err := run(f, newUpdateCmd(f), db.Name, "--add-logical-database", "analytics", "--add-schema", "analytics.reporting", "--add-schema", "local", "--wait=false")
	if err != nil {
		t.Fatal(err)
	}
	want := []client.LogicalDatabase{entry("source_first", "keep", "local"), entry("warehouse", "raw"), entry("analytics", "reporting")}
	got := fake.DatabasesByName[db.Name]
	if !reflect.DeepEqual(*got.Postgres.Databases, want) || !reflect.DeepEqual(*got.Postgres.Roles, roles) || !reflect.DeepEqual(*got.Postgres.Extensions, extensions) {
		t.Fatalf("lost declarations: %+v", got.Postgres)
	}
	f = factoryWith(t, fake)
	before := len(fake.Calls)
	_, stderr, err := run(f, newUpdateCmd(f), db.Name, "--add-logical-database", "analytics", "--add-schema", "analytics.reporting", "--wait=false")
	if err != nil || !strings.Contains(stderr, "already exists") {
		t.Fatalf("duplicate: %s %v", stderr, err)
	}
	for _, call := range fake.Calls[before:] {
		if strings.HasPrefix(call, "UpdateDatabase:") {
			t.Fatalf("duplicate patched: %v", fake.Calls[before:])
		}
	}
}

func TestUpdate_AddSchemaToUnknownDatabaseIsMisuse(t *testing.T) {
	fake := clienttest.New()
	fake.DatabasesByName["orders"] = logicalDB("orders", entry("source_first"))
	f := factoryWith(t, fake)
	_, _, err := run(f, newUpdateCmd(f), "orders", "--add-schema", "absent.reporting", "--wait=false")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(err.Error(), "source_first") {
		t.Fatal(err)
	}
	assertNoCall(t, fake, "UpdateDatabase:")
}

func TestUpdate_AbsentLogicalFieldsRefusesWithoutPatch(t *testing.T) {
	fake := clienttest.New()
	fake.DatabasesByName["restored"] = running("restored")
	f := factoryWith(t, fake)
	_, _, err := run(f, newUpdateCmd(f), "restored", "--add-logical-database", "analytics", "--wait=false")
	assertExit(t, err, cli.ExitUnavailable)
	assertNoCall(t, fake, "UpdateDatabase:")
}

func TestBindingAdd_LogicalDatabase(t *testing.T) {
	for _, tt := range []struct{ selector, want string }{{"analytics_rollup", "orders-db-analytics-rollup-prod-readwrite"}, {"source_first", "orders-db-prod-readwrite"}, {"", "orders-db-prod-readwrite"}} {
		t.Run(tt.selector, func(t *testing.T) {
			fake := clienttest.New()
			fake.DatabasesByName["orders-db"] = logicalDB("orders-db", entry("source_first"), entry("analytics_rollup"))
			f := factoryWith(t, fake)
			args := []string{"orders-db", "--cluster", "prod", "--namespace", "reports", "--role", "readwrite", "--wait=false", "-o", "json"}
			if tt.selector != "" {
				args = append(args, "--logical-database", tt.selector)
			}
			out, _, err := run(f, newBindingAddCmd(f), args...)
			if err != nil {
				t.Fatal(err)
			}
			var b client.DatabaseBinding
			if err := json.Unmarshal([]byte(out), &b); err != nil {
				t.Fatal(err)
			}
			wantLogical := tt.selector
			if wantLogical == "" {
				wantLogical = "source_first"
			}
			if b.Name != tt.want || b.LogicalDatabase != wantLogical || strings.Contains(b.Name, "reports") {
				t.Fatalf("binding %+v", b)
			}
		})
	}
}

func TestBindingAdd_LogicalDefaultNameClash(t *testing.T) {
	for _, tt := range []struct {
		status        int
		message, want string
	}{{409, "binding already exists", "--name"}, {400, `secret name "orders-analytics-readwrite" is already used; set spec.secretName to another name`, "--secret-name"}} {
		fake := clienttest.New()
		fake.DatabasesByName["orders"] = logicalDB("orders", entry("orders"), entry("analytics"))
		fake.CreateDatabaseBindingErr = &client.APIError{StatusCode: tt.status, Message: tt.message}
		f := factoryWith(t, fake)
		_, _, err := run(f, newBindingAddCmd(f), "orders", "--logical-database", "analytics", "--cluster", "prod", "--namespace", "reports", "--role", "readwrite", "--wait=false")
		if err == nil || !strings.Contains(hint(err), tt.want) || strings.Contains(hint(err), "binding update") || !strings.Contains(err.Error(), tt.message) {
			t.Fatalf("err=%v hint=%s", err, hint(err))
		}
	}
}

func TestLogicalWaits(t *testing.T) {
	for _, creating := range []bool{true, false} {
		t.Run(fmt.Sprint(creating), func(t *testing.T) {
			fake := clienttest.New()
			db := logicalDB("orders", entry("orders"), entry("analytics"))
			db.Generation = 2
			db.Status.ObservedGeneration = 2
			if !creating {
				fake.DatabasesByName["orders"] = logicalDB("orders", entry("orders"))
			}
			stale := *db
			status := *db.Status
			stale.Status = &status
			stale.Status.ObservedGeneration = 1
			pending := *db
			status2 := *db.Status
			pending.Status = &status2
			pending.Status.Databases = []client.LogicalDatabaseStatus{{Name: "orders", Phase: client.PhaseReady}, {Name: "analytics", Phase: "Pending"}}
			fake.GetDatabaseSeq["orders"] = []*client.Database{&stale, &pending, db}
			if !creating {
				initial := fake.DatabasesByName["orders"]
				initial.Generation = 2
				fake.GetDatabaseSeq["orders"] = append([]*client.Database{initial}, fake.GetDatabaseSeq["orders"]...)
			}
			f := factoryWith(t, fake)
			var err error
			if creating {
				_, _, err = run(f, newCreateCmd(f), "orders", "--cpu", "500m", "--memory", "1Gi", "--storage", "5Gi", "--logical-database", "analytics", "--wait-timeout", "10s")
			} else {
				_, _, err = run(f, newUpdateCmd(f), "orders", "--add-logical-database", "analytics", "--wait-timeout", "10s")
			}
			if err != nil {
				t.Fatal(err)
			}
			wantPolls := 3
			if !creating {
				wantPolls++
			}
			if countCalls(fake, "GetDatabase:orders") != wantPolls {
				t.Fatalf("returned before converged: %d polls remain", len(fake.GetDatabaseSeq["orders"]))
			}
		})
	}
}

func TestLogicalFailedWaitReportsMessage(t *testing.T) {
	for _, creating := range []bool{true, false} {
		t.Run(fmt.Sprint(creating), func(t *testing.T) {
			fake := clienttest.New()
			db := logicalDB("orders", entry("orders"), entry("analytics"))
			db.Status.Databases[1] = client.LogicalDatabaseStatus{Name: "analytics", Phase: client.PhaseFailed, Message: "schema creation refused"}
			if !creating {
				fake.DatabasesByName["orders"] = logicalDB("orders", entry("orders"))
			}
			fake.GetDatabaseSeq["orders"] = []*client.Database{db}
			if !creating {
				fake.GetDatabaseSeq["orders"] = append([]*client.Database{fake.DatabasesByName["orders"]}, fake.GetDatabaseSeq["orders"]...)
			}
			f := factoryWith(t, fake)
			var err error
			if creating {
				_, _, err = run(f, newCreateCmd(f), "orders", "--cpu", "500m", "--memory", "1Gi", "--storage", "5Gi", "--logical-database", "analytics", "--wait-timeout", "100ms")
			} else {
				_, _, err = run(f, newUpdateCmd(f), "orders", "--add-logical-database", "analytics", "--wait-timeout", "100ms")
			}
			assertExit(t, err, cli.ExitGeneral)
			if !strings.Contains(hint(err), "schema creation refused") {
				t.Fatalf("lost failure message: %v %s", err, hint(err))
			}
		})
	}
}

func TestBindingAdd_LogicalLocalBounds(t *testing.T) {
	for _, selector := range []string{strings.Repeat("a", 54), "Upper", "pg.bad"} {
		fake := clienttest.New()
		f := factoryWith(t, fake)
		_, _, err := run(f, newBindingAddCmd(f), "orders", "--cluster", "prod", "--namespace", "reports", "--role", "owner", "--logical-database", selector, "--wait=false")
		assertExit(t, err, cli.ExitMisuse)
		if len(fake.Calls) != 0 {
			t.Fatalf("local invalid name called API: %v", fake.Calls)
		}
	}
	fake := clienttest.New()
	selector := strings.Repeat("a", 53)
	fake.DatabasesByName["orders"] = logicalDB("orders", entry("orders"), entry(selector))
	f := factoryWith(t, fake)
	_, _, err := run(f, newBindingAddCmd(f), "orders", "--cluster", "prod", "--namespace", "reports", "--role", "owner", "--logical-database", selector, "--wait=false")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(hint(err), "--name") {
		t.Fatal(err)
	}
	assertNoCall(t, fake, "CreateDatabaseBinding:")
}

func TestWaitRunningUnchangedWithFailedLogicalEntry(t *testing.T) {
	fake := clienttest.New()
	db := logicalDB("orders", entry("orders"), entry("analytics"))
	db.Status.Databases[1].Phase = client.PhaseFailed
	fake.DatabasesByName["orders"] = db
	f := factoryWith(t, fake)
	_, _, err := run(f, newWaitCmd(f), "orders", "--for", "running", "--timeout", "100ms")
	if err != nil {
		t.Fatalf("running phase wait changed: %v", err)
	}
}

// Extracted from the frozen 4a chart's services publication, not hand-copied
// limits. Platform overrides smaller than these remain server-authoritative.
func TestLogicalPublishedPolicyMirror(t *testing.T) {
	raw, err := os.ReadFile("testdata/logical-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Reserved   []string `json:"postgresReservedNames"`
		Added      []string `json:"postgresReservedLogicalNames"`
		Prefixes   []string `json:"postgresReservedLogicalPrefixes"`
		Databases  int      `json:"maxLogicalDatabases"`
		Schemas    int      `json:"maxSchemasPerDatabase"`
		NameLength int      `json:"maxLogicalNameLength"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Databases != maxLogicalDatabases || policy.Schemas != maxDatabaseSchemas || policy.NameLength != 53 || !reflect.DeepEqual(policy.Reserved, reservedDatabaseNames) {
		t.Fatalf("published bounds/reservations drift: %+v", policy)
	}
	got := slices.Clone(reservedLogicalPrefixes)
	slices.Sort(got)
	want := slices.Clone(policy.Prefixes)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("published prefixes drift: %v %v", got, want)
	}
	for _, name := range policy.Reserved {
		if validateLogicalName(name) == nil {
			t.Errorf("system name accepted: %s", name)
		}
	}
	for _, name := range policy.Added {
		if validateAddedLogicalName(name) == nil {
			t.Errorf("added reserved name accepted: %s", name)
		}
	}
	for _, prefix := range policy.Prefixes {
		for _, name := range []string{prefix + "reports", strings.TrimSuffix(prefix, "_")} {
			if validateAddedLogicalName(name) == nil {
				t.Errorf("reserved prefix accepted: %s", name)
			}
		}
	}
}
