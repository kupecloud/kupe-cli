package database

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestUpdate_LogicalHTTPPreconditionPreservesConcurrentAppend(t *testing.T) {
	for _, contention := range []bool{false, true} {
		t.Run(fmt.Sprint(contention), func(t *testing.T) {
			reads, patches := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/tenants/acme/databases/restored" {
					t.Errorf("path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				switch r.Method {
				case http.MethodGet:
					reads++
					w.Header().Set("ETag", fmt.Sprintf(`"opaque-%d"`, reads))
					list := []client.LogicalDatabase{entry("source_first", "keep"), entry("warehouse", "raw")}
					if reads > 1 {
						list = append(list, entry("concurrent", "other"))
					}
					db := logicalDB("restored", list...)
					roles := []client.DeclaredRole{{Name: "auditor"}}
					ext := []client.Extension{{Name: "pg_trgm"}}
					db.Postgres.Roles = &roles
					db.Postgres.Extensions = &ext
					if err := json.NewEncoder(w).Encode(db); err != nil {
						t.Error(err)
					}
				case http.MethodPatch:
					patches++
					if got := r.Header.Get("If-Match"); got != fmt.Sprintf(`"opaque-%d"`, patches) {
						t.Errorf("If-Match=%q", got)
					}
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body) != 1 || body["postgres"] == nil {
						t.Errorf("body=%s", body)
					}
					var pg map[string]json.RawMessage
					if err := json.Unmarshal(body["postgres"], &pg); err != nil {
						t.Error(err)
					}
					if len(pg) != 1 || pg["databases"] == nil {
						t.Errorf("unrequested lists: %s", pg)
					}
					var list []client.LogicalDatabase
					if err := json.Unmarshal(pg["databases"], &list); err != nil {
						t.Error(err)
					}
					want := []client.LogicalDatabase{entry("source_first", "keep"), entry("warehouse", "raw")}
					if patches > 1 {
						want = append(want, entry("concurrent", "other"))
					}
					want = append(want, entry("analytics", "rollup"))
					if !reflect.DeepEqual(list, want) {
						t.Errorf("PATCH dropped or changed entries: got %+v want %+v", list, want)
					}
					if patches == 1 || contention {
						w.WriteHeader(412)
						fmt.Fprintln(w, `{"error":"version changed"}`)
						return
					}
					if err := json.NewEncoder(w).Encode(logicalDB("restored", list...)); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("method %s", r.Method)
				}
			}))
			defer srv.Close()
			f := factoryWith(t, clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "synthetic", "test"), nil }
			out, _, err := run(f, newUpdateCmd(f), "restored", "--add-logical-database", "analytics", "--add-schema", "analytics.rollup", "--wait=false", "-o", "json")
			if contention {
				assertExit(t, err, cli.ExitConflict)
			} else if err != nil || !strings.Contains(out, "concurrent") {
				t.Fatalf("out=%s err=%v", out, err)
			}
			if reads != 2 || patches != 2 {
				t.Fatalf("reads=%d patches=%d", reads, patches)
			}
		})
	}
}

func TestCreate_LogicalHTTPWireAndOldAPI(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(fmt.Sprint(old), func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes++
				if r.Method != "POST" || r.URL.Path != "/api/v1/tenants/acme/databases" {
					t.Errorf("%s %s", r.Method, r.URL.Path)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["databases"] != nil || body["source"] != nil {
					t.Errorf("unexpected top-level/source: %s", body)
				}
				var pg client.DatabasePostgres
				if err := json.Unmarshal(body["postgres"], &pg); err != nil {
					t.Error(err)
				}
				if pg.Databases == nil || !reflect.DeepEqual(*pg.Databases, []client.LogicalDatabase{entry("orders_db", "reporting"), entry("analytics")}) {
					t.Errorf("postgres=%+v", pg)
				}
				if pg.Roles == nil || len(*pg.Roles) != 0 || pg.Extensions == nil || len(*pg.Extensions) != 0 {
					t.Errorf("explicit [] dropped: %+v", pg)
				}
				if old {
					w.Header().Set("X-Request-Id", "old-platform")
					w.WriteHeader(400)
					fmt.Fprintln(w, `{"error":"json: unknown field \"databases\""}`)
					return
				}
				w.WriteHeader(201)
				if err := json.NewEncoder(w).Encode(logicalDB("orders-db", (*pg.Databases)...)); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			f := factoryWith(t, clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "synthetic", "test"), nil }
			_, _, err := run(f, newCreateCmd(f), "orders-db", "--cpu", "500m", "--memory", "1Gi", "--storage", "5Gi", "--logical-database", "analytics", "--schema", "reporting", "--roles=", "--extensions=", "--wait=false")
			if old {
				assertExit(t, err, cli.ExitUnavailable)
				if !strings.Contains(hint(err), "does not offer logical") || !strings.Contains(err.Error(), "unknown field") {
					t.Fatalf("%v %s", err, hint(err))
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Fatalf("replayed %d writes", writes)
			}
		})
	}
}

func TestCreate_RestoreHTTPPreservesSourceAndListPresence(t *testing.T) {
	for _, source := range []string{"--from", "--from-backup"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", source, explicit), func(t *testing.T) {
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != "POST" {
						t.Errorf("unexpected source read: %s", r.Method)
					}
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					var src client.DatabaseSource
					if err := json.Unmarshal(body["source"], &src); err != nil {
						t.Error(err)
					}
					if source == "--from" && src.Database != "original" || source == "--from-backup" && src.Backup != "original" {
						t.Errorf("source=%+v", src)
					}
					if explicit {
						if string(body["postgres"]) != `{"roles":[],"extensions":[]}` {
							t.Errorf("restore list presence=%s", body["postgres"])
						}
					} else if body["postgres"] != nil {
						t.Errorf("invented postgres=%s", body["postgres"])
					}
					w.WriteHeader(201)
					fmt.Fprintln(w, `{"name":"restored"}`)
				}))
				defer srv.Close()
				f := factoryWith(t, clienttest.New())
				f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "synthetic", "test"), nil }
				args := []string{"restored", "--cpu", "500m", "--memory", "1Gi", "--storage", "5Gi", source, "original", "--wait=false"}
				if explicit {
					args = append(args, "--roles=", "--extensions=")
				}
				_, _, err := run(f, newCreateCmd(f), args...)
				if err != nil || calls != 1 {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			})
		}
	}
}

func TestBindingAdd_LogicalHTTPWireAndOldAPI(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(fmt.Sprint(old), func(t *testing.T) {
			gets, posts := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					gets++
					if err := json.NewEncoder(w).Encode(logicalDB("orders", entry("source_first"), entry("analytics"))); err != nil {
						t.Error(err)
					}
				case http.MethodPost:
					posts++
					var req client.CreateDatabaseBindingRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
					}
					if req.LogicalDatabase != "analytics" || req.Name != "orders-analytics-prod-readonly" || req.SecretName != "" {
						t.Errorf("req=%+v", req)
					}
					if old {
						w.WriteHeader(400)
						fmt.Fprintln(w, `{"error":"json: unknown field \"logicalDatabase\""}`)
						return
					}
					w.WriteHeader(201)
					fmt.Fprintln(w, `{"name":"orders-analytics-prod-readonly","logicalDatabase":"analytics"}`)
				default:
					t.Errorf("unexpected %s", r.Method)
				}
			}))
			defer srv.Close()
			f := factoryWith(t, clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "synthetic", "test"), nil }
			_, _, err := run(f, newBindingAddCmd(f), "orders", "--cluster", "prod", "--namespace", "reports", "--role", "readonly", "--logical-database", "analytics", "--wait=false")
			if old {
				assertExit(t, err, cli.ExitUnavailable)
				if !strings.Contains(hint(err), "does not offer logical") {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if gets != 1 || posts != 1 {
				t.Fatalf("gets=%d posts=%d", gets, posts)
			}
		})
	}
}
