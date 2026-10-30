package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
)

// databaseJSON is a representative GET /databases/{name} body.
const databaseJSON = `{"name":"orders-db","engine":"postgres","version":"18",
  "resources":{"cpu":"500m","memory":"1Gi","storage":"10Gi"},
  "highAvailability":false,"deletionProtection":true,
  "status":{"observedGeneration":2,"phase":"Running","currentVersion":"18.0","instancesReady":1,"instancesDesired":1,
    "conditions":[{"type":"Ready","status":"True","reason":"Running"}]},
  "generation":2,"resourceVersion":"582019","createdAt":"2026-10-09T10:00:00Z"}`

// expectRequest fails the test unless r has the given method and path.
func expectRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method || r.URL.Path != path {
		t.Errorf("request = %s %s; want %s %s", r.Method, r.URL.Path, method, path)
	}
}

// decodeBody decodes the request body into a generic map for assertions on
// exactly which fields were sent.
func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding body %q: %v", raw, err)
	}
	return body
}

func TestListDatabases_DecodesItems(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/databases")
		fmt.Fprintf(w, `{"items":[%s]}`, databaseJSON)
	})
	list, err := c.Databases().List(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("items = %d; want 1", len(list))
	}
	db := list[0]
	if db.Name != "orders-db" || db.Resources.Memory != "1Gi" || !db.DeletionProtection ||
		db.Status.CurrentVersion != "18.0" || db.Status.InstancesDesired != 1 || db.Status.Conditions[0].Type != "Ready" {
		t.Fatalf("decoded database = %+v", db)
	}
}

func TestGetDatabase_ReturnsETag(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/databases/orders-db")
		w.Header().Set("ETag", "582019")
		fmt.Fprintln(w, databaseJSON)
	})
	db, etag, err := c.Databases().Get(context.Background(), "orders-db")
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if etag != "582019" || db.Version != "18" {
		t.Fatalf("got etag=%q db=%+v", etag, db)
	}
}

func TestCreateDatabase_SendsOnlySetFields(t *testing.T) {
	off := false
	days := int32(14)
	resources := &DatabaseResources{CPU: "500m", Memory: "1Gi", Storage: "10Gi"}
	tests := []struct {
		name       string
		req        CreateDatabaseRequest
		want       map[string]bool // field → present in body
		wantDP     any
		wantVer    any
		wantBackup map[string]any // the backup object sent, when present
	}{
		{
			name: "defaults leave version, HA, deletion protection and backups to the server",
			req:  CreateDatabaseRequest{Name: "orders-db", Engine: "postgres", Resources: resources},
			want: map[string]bool{"version": false, "highAvailability": false, "deletionProtection": false, "backup": false},
		},
		{
			name:       "a retention alone sends only the retention",
			req:        CreateDatabaseRequest{Name: "orders-db", Resources: resources, Backup: &DatabaseBackup{Retention: &days}},
			want:       map[string]bool{"backup": true},
			wantBackup: map[string]any{"retention": float64(14)},
		},
		{
			name:       "a schedule alone sends only the schedule",
			req:        CreateDatabaseRequest{Name: "orders-db", Resources: resources, Backup: &DatabaseBackup{Schedule: "DailyThenWeekly"}},
			want:       map[string]bool{"backup": true},
			wantBackup: map[string]any{"schedule": "DailyThenWeekly"},
		},
		{
			name:       "retention and schedule are both sent",
			req:        CreateDatabaseRequest{Name: "orders-db", Resources: resources, Backup: &DatabaseBackup{Retention: &days, Schedule: "Daily"}},
			want:       map[string]bool{"backup": true},
			wantBackup: map[string]any{"retention": float64(14), "schedule": "Daily"},
		},
		{
			name: "explicit version, HA and disabled protection are sent",
			req: CreateDatabaseRequest{Name: "orders-db", Engine: "postgres", Version: "17", HighAvailability: true,
				DeletionProtection: &off, Resources: &DatabaseResources{CPU: "1", Memory: "2Gi", Storage: "20Gi"}},
			want:    map[string]bool{"version": true, "highAvailability": true, "deletionProtection": true},
			wantDP:  false,
			wantVer: "17",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases")
				body := decodeBody(t, r)
				for field, present := range tt.want {
					if _, ok := body[field]; ok != present {
						t.Errorf("field %q present=%v; want %v (body %v)", field, ok, present, body)
					}
				}
				if tt.wantDP != nil && body["deletionProtection"] != tt.wantDP {
					t.Errorf("deletionProtection = %v; want %v", body["deletionProtection"], tt.wantDP)
				}
				if tt.wantVer != nil && body["version"] != tt.wantVer {
					t.Errorf("version = %v; want %v", body["version"], tt.wantVer)
				}
				if tt.wantBackup != nil && !reflect.DeepEqual(body["backup"], tt.wantBackup) {
					t.Errorf("backup = %v; want %v", body["backup"], tt.wantBackup)
				}
				w.Header().Set("ETag", "1")
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintln(w, databaseJSON)
			})
			db, etag, err := c.Databases().Create(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("CreateDatabase: %v", err)
			}
			if db.Name != "orders-db" || etag != "1" {
				t.Fatalf("got db=%+v etag=%q", db, etag)
			}
		})
	}
}

func TestCreateDatabase_ConflictIsConflict(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"database already exists"}`)
	})
	_, _, err := c.Databases().Create(context.Background(), CreateDatabaseRequest{Name: "orders-db"})
	if !IsConflict(err) {
		t.Fatalf("want IsConflict, got %v", err)
	}
}

func TestUpdateDatabase_SendsIfMatchAndOnlySetFields(t *testing.T) {
	off := false
	days := int32(21)
	tests := []struct {
		name  string
		patch PatchDatabaseRequest
		want  map[string]any // the whole body
	}{
		{"deletion protection alone", PatchDatabaseRequest{DeletionProtection: &off},
			map[string]any{"deletionProtection": false}},
		{"a retention alone leaves the schedule out", PatchDatabaseRequest{Backup: &DatabaseBackup{Retention: &days}},
			map[string]any{"backup": map[string]any{"retention": float64(21)}}},
		{"a schedule alone leaves the retention out", PatchDatabaseRequest{Backup: &DatabaseBackup{Schedule: "DailyThenWeekly"}},
			map[string]any{"backup": map[string]any{"schedule": "DailyThenWeekly"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPatch, "/api/v1/tenants/acme/databases/orders-db")
				if got := r.Header.Get("If-Match"); got != "7" {
					t.Errorf("If-Match = %q; want 7", got)
				}
				if body := decodeBody(t, r); !reflect.DeepEqual(body, tt.want) {
					t.Errorf("body = %v; want %v", body, tt.want)
				}
				w.Header().Set("ETag", "8")
				fmt.Fprintln(w, databaseJSON)
			})
			_, etag, err := c.Databases().Update(context.Background(), "orders-db", "7", tt.patch)
			if err != nil {
				t.Fatalf("UpdateDatabase: %v", err)
			}
			if etag != "8" {
				t.Fatalf("etag = %q; want 8", etag)
			}
		})
	}
}

// TestDatabase_DecodesBackupStatus pins the backup keys of kupe-api's
// database response: the settings, and the status the backups report. A
// database that never set them, or has no backup yet, sends backup null,
// lastBackupAt "", recoveryWindow null and backupSizeBytes 0.
func TestDatabase_DecodesBackupStatus(t *testing.T) {
	days := int32(14)
	tests := []struct {
		name string
		body string
		want func(*Database) bool
	}{
		{"settings and status",
			`{"name":"orders-db","backup":{"retention":14,"schedule":"DailyThenWeekly"},
			  "status":{"phase":"Running","lastBackupAt":"2026-10-16T02:14:07Z",
			    "recoveryWindow":{"earliest":"2026-10-02T02:14:07Z","latest":"2026-10-16T10:41:33Z"},
			    "backupSizeBytes":3435973837}}`,
			func(db *Database) bool {
				return reflect.DeepEqual(db.Backup, &DatabaseBackup{Retention: &days, Schedule: "DailyThenWeekly"}) &&
					db.Status.LastBackupAt == "2026-10-16T02:14:07Z" &&
					reflect.DeepEqual(db.Status.RecoveryWindow, &RecoveryWindow{Earliest: "2026-10-02T02:14:07Z", Latest: "2026-10-16T10:41:33Z"}) &&
					db.Status.BackupSizeBytes == 3435973837
			}},
		{"a schedule without a retention leaves the retention to the default",
			`{"name":"orders-db","backup":{"schedule":"Daily"},"status":{"phase":"Running"}}`,
			func(db *Database) bool {
				return db.Backup != nil && db.Backup.Retention == nil && db.Backup.Schedule == "Daily"
			}},
		{"never set and no backup yet",
			`{"name":"orders-db","backup":null,
			  "status":{"phase":"Running","lastBackupAt":"","recoveryWindow":null,"backupSizeBytes":0}}`,
			func(db *Database) bool {
				return db.Backup == nil && db.Status.LastBackupAt == "" && db.Status.RecoveryWindow == nil &&
					db.Status.BackupSizeBytes == 0
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, tt.body)
			})
			db, _, err := c.Databases().Get(context.Background(), "orders-db")
			if err != nil {
				t.Fatalf("GetDatabase: %v", err)
			}
			if !tt.want(db) {
				t.Fatalf("decoded backup = %+v, status = %+v", db.Backup, db.Status)
			}
		})
	}
}

func TestUpdateDatabaseRMW(t *testing.T) {
	v18 := "18"
	tests := []struct {
		name        string
		patchStatus []int // response status per PATCH attempt
		mutate      func(*Database) *PatchDatabaseRequest
		wantPatches int32
		wantErr     func(error) bool
	}{
		{
			name:        "succeeds on first PATCH",
			patchStatus: []int{http.StatusOK},
			mutate:      func(*Database) *PatchDatabaseRequest { return &PatchDatabaseRequest{Version: &v18} },
			wantPatches: 1,
		},
		{
			name:        "retries once after a 412",
			patchStatus: []int{http.StatusPreconditionFailed, http.StatusOK},
			mutate:      func(*Database) *PatchDatabaseRequest { return &PatchDatabaseRequest{Version: &v18} },
			wantPatches: 2,
		},
		{
			name:        "two 412s return contention that still maps to a conflict",
			patchStatus: []int{http.StatusPreconditionFailed, http.StatusPreconditionFailed},
			mutate:      func(*Database) *PatchDatabaseRequest { return &PatchDatabaseRequest{Version: &v18} },
			wantPatches: 2,
			wantErr: func(err error) bool {
				return errors.Is(err, ErrRMWContention) && IsPreconditionFailed(err)
			},
		},
		{
			name:        "a webhook refusal is returned verbatim without retry",
			patchStatus: []int{http.StatusBadRequest},
			mutate:      func(*Database) *PatchDatabaseRequest { return &PatchDatabaseRequest{Version: &v18} },
			wantPatches: 1,
			wantErr: func(err error) bool {
				return IsValidation(err) && err.Error() == "major version upgrades are not available yet"
			},
		},
		{
			name:        "nil patch is a no-op",
			mutate:      func(*Database) *PatchDatabaseRequest { return nil },
			wantPatches: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patches int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.Header().Set("ETag", "5")
					fmt.Fprintln(w, databaseJSON)
					return
				}
				n := atomic.AddInt32(&patches, 1)
				if got := r.Header.Get("If-Match"); got != "5" {
					t.Errorf("If-Match = %q; want 5", got)
				}
				status := tt.patchStatus[n-1]
				w.WriteHeader(status)
				switch status {
				case http.StatusPreconditionFailed:
					fmt.Fprintln(w, `{"error":"etag mismatch"}`)
				case http.StatusBadRequest:
					fmt.Fprintln(w, `{"error":"major version upgrades are not available yet"}`)
				default:
					fmt.Fprintln(w, databaseJSON)
				}
			})
			db, err := c.Databases().UpdateRMW(context.Background(), "orders-db", tt.mutate)
			if tt.wantErr != nil {
				if !tt.wantErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err != nil || db == nil {
				t.Fatalf("RMW: db=%v err=%v", db, err)
			}
			if got := atomic.LoadInt32(&patches); got != tt.wantPatches {
				t.Fatalf("PATCH attempts = %d; want %d", got, tt.wantPatches)
			}
		})
	}
}

func TestDeleteDatabase_ErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantErr  func(error) bool
		wantCode string
	}{
		{name: "204 succeeds", status: http.StatusNoContent},
		{
			name:     "409 deletion_protected carries the code",
			status:   http.StatusConflict,
			body:     `{"error":"database \"orders-db\" has deletionProtection enabled","code":"deletion_protected","severity":"error","message":"database \"orders-db\" has deletionProtection enabled"}`,
			wantErr:  IsConflict,
			wantCode: DatabaseDeleteCodeProtected,
		},
		{name: "404 is not found", status: http.StatusNotFound, body: `{"error":"database not found"}`, wantErr: IsNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodDelete, "/api/v1/tenants/acme/databases/orders-db")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})
			err := c.Databases().Delete(context.Background(), "orders-db")
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("DeleteDatabase: %v", err)
				}
				return
			}
			if !tt.wantErr(err) {
				t.Fatalf("unexpected error class: %v", err)
			}
			if got := ErrorCode(err); got != tt.wantCode {
				t.Fatalf("ErrorCode = %q; want %q", got, tt.wantCode)
			}
		})
	}
}

func TestRotateDatabaseCredentials_PostsRoleAndDecodes202(t *testing.T) {
	for _, role := range DatabaseRoles {
		t.Run(role, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases/orders-db/rotate-credentials")
				if body := decodeBody(t, r); len(body) != 1 || body["role"] != role {
					t.Errorf("body = %v; want {role: %s}", body, role)
				}
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprintf(w, `{"database":"orders-db","role":%q,"requestedAt":"2026-10-09T12:00:00Z"}`+"\n", role)
			})
			rot, err := c.RotateDatabaseCredentials(context.Background(), "orders-db", role)
			if err != nil {
				t.Fatalf("RotateDatabaseCredentials: %v", err)
			}
			if rot.Database != "orders-db" || rot.Role != role || rot.RequestedAt == "" {
				t.Fatalf("rotation = %+v", rot)
			}
		})
	}
}

func TestRotateDatabaseCredentials_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"database not found"}`)
	})
	if _, err := c.RotateDatabaseCredentials(context.Background(), "ghost", DatabaseRoleOwner); !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}

const bindingJSON = `{"name":"orders-db-prod-readwrite","database":"orders-db","cluster":"prod","namespaces":["api","jobs"],
  "role":"readwrite","secretName":"","status":{"phase":"Pending","secretName":"orders-db-readwrite",
  "message":"the database is not yet available in namespace \"jobs\"; retrying",
  "namespaces":[{"name":"api","host":"orders-db.api.svc.cluster.local","phase":"Ready","deliveredRotation":"2026-10-09T12:00:00Z","message":""},
    {"name":"jobs","host":"","phase":"Pending","deliveredRotation":"","message":"the database is not yet available"}],
  "conditions":[]},
  "resourceVersion":"582020","createdAt":"2026-10-09T10:05:00Z"}`

func TestListDatabaseBindings(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/databases/orders-db/bindings")
		fmt.Fprintf(w, `{"items":[%s]}`, bindingJSON)
	})
	list, err := c.ListDatabaseBindings(context.Background(), "orders-db")
	if err != nil {
		t.Fatalf("ListDatabaseBindings: %v", err)
	}
	if len(list) != 1 || list[0].Status.SecretName != "orders-db-readwrite" {
		t.Fatalf("decoded bindings = %+v", list)
	}
	want := []DatabaseBindingNamespaceStatus{
		{Name: "api", Host: "orders-db.api.svc.cluster.local", Phase: "Ready", DeliveredRotation: "2026-10-09T12:00:00Z"},
		{Name: "jobs", Phase: "Pending", Message: "the database is not yet available"},
	}
	if got := list[0]; !slices.Equal(got.Namespaces, []string{"api", "jobs"}) || !slices.Equal(got.Status.Namespaces, want) {
		t.Fatalf("namespaces = %v, status = %+v", got.Namespaces, got.Status.Namespaces)
	}
}

func TestListDatabaseBindings_MissingDatabaseIsNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"database not found"}`)
	})
	if _, err := c.ListDatabaseBindings(context.Background(), "ghost"); !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}

func TestGetDatabaseBinding(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/databases/orders-db/bindings/orders-db-prod-readwrite")
		fmt.Fprintln(w, bindingJSON)
	})
	b, err := c.GetDatabaseBinding(context.Background(), "orders-db", "orders-db-prod-readwrite")
	if err != nil {
		t.Fatalf("GetDatabaseBinding: %v", err)
	}
	if b.Cluster != "prod" || b.Role != "readwrite" {
		t.Fatalf("binding = %+v", b)
	}
}

func TestCreateDatabaseBinding(t *testing.T) {
	tests := []struct {
		name           string
		req            CreateDatabaseBindingRequest
		wantSecretSent bool
	}{
		{"secret name omitted when empty", CreateDatabaseBindingRequest{Name: "b", Cluster: "prod", Namespaces: []string{"api", "jobs"}, Role: "readwrite"}, false},
		{"secret name sent when set", CreateDatabaseBindingRequest{Name: "b", Cluster: "prod", Namespaces: []string{"api", "jobs"}, Role: "readwrite", SecretName: "pg"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases/orders-db/bindings")
				body := decodeBody(t, r)
				if _, ok := body["secretName"]; ok != tt.wantSecretSent {
					t.Errorf("secretName present=%v; want %v", ok, tt.wantSecretSent)
				}
				if body["cluster"] != "prod" || body["role"] != "readwrite" || body["name"] != "b" {
					t.Errorf("body = %v", body)
				}
				if ns, _ := json.Marshal(body["namespaces"]); string(ns) != `["api","jobs"]` {
					t.Errorf("namespaces = %s; want [\"api\",\"jobs\"]", ns)
				}
				if _, old := body["namespace"]; old {
					t.Errorf("body carries the old namespace field: %v", body)
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintln(w, bindingJSON)
			})
			if _, err := c.CreateDatabaseBinding(context.Background(), "orders-db", tt.req); err != nil {
				t.Fatalf("CreateDatabaseBinding: %v", err)
			}
		})
	}
}

// TestUpdateDatabaseBindingRMW pins the read-edit-write the binding PATCH
// needs: the body is only the new namespace list, If-Match carries the ETag
// of the read the list was edited from, and a 412 re-reads and edits again.
func TestUpdateDatabaseBindingRMW(t *testing.T) {
	const path = "/api/v1/tenants/acme/databases/orders-db/bindings/orders-db-prod-readwrite"
	addReports := func(b *DatabaseBinding) *PatchDatabaseBindingRequest {
		return &PatchDatabaseBindingRequest{Namespaces: append(slices.Clone(b.Namespaces), "reports")}
	}
	tests := []struct {
		name        string
		patchStatus []int
		mutate      DatabaseBindingMutator
		wantPatches int32
		wantErr     func(error) bool
	}{
		{"one PATCH", []int{http.StatusOK}, addReports, 1, nil},
		{"a 412 re-reads and retries once", []int{http.StatusPreconditionFailed, http.StatusOK}, addReports, 2, nil},
		{"two 412s are contention", []int{http.StatusPreconditionFailed, http.StatusPreconditionFailed}, addReports, 2,
			func(err error) bool { return errors.Is(err, ErrRMWContention) && IsPreconditionFailed(err) }},
		{"nil patch sends nothing", nil, func(*DatabaseBinding) *PatchDatabaseBindingRequest { return nil }, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gets, patches int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path {
					t.Errorf("path = %s; want %s", r.URL.Path, path)
				}
				if r.Method == http.MethodGet {
					n := atomic.AddInt32(&gets, 1)
					w.Header().Set("ETag", fmt.Sprint(582020+n))
					fmt.Fprintln(w, bindingJSON)
					return
				}
				n := atomic.AddInt32(&patches, 1)
				if r.Method != http.MethodPatch {
					t.Errorf("method = %s; want PATCH", r.Method)
				}
				if got, want := r.Header.Get("If-Match"), fmt.Sprint(582020+atomic.LoadInt32(&gets)); got != want {
					t.Errorf("If-Match = %q; want %q, the ETag of the latest read", got, want)
				}
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != `{"namespaces":["api","jobs","reports"]}` {
					t.Errorf("body = %s; want only the new namespace list", raw)
				}
				status := tt.patchStatus[n-1]
				w.WriteHeader(status)
				if status == http.StatusPreconditionFailed {
					fmt.Fprintln(w, `{"error":"resource has been modified (ETag mismatch)"}`)
					return
				}
				fmt.Fprintln(w, bindingJSON)
			})
			b, err := c.UpdateDatabaseBindingRMW(context.Background(), "orders-db", "orders-db-prod-readwrite", tt.mutate)
			if tt.wantErr != nil {
				if !tt.wantErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err != nil || b == nil {
				t.Fatalf("RMW: binding=%v err=%v", b, err)
			}
			if got := atomic.LoadInt32(&patches); got != tt.wantPatches {
				t.Fatalf("PATCH attempts = %d; want %d", got, tt.wantPatches)
			}
		})
	}
}

func TestCreateDatabaseBinding_ConflictIsConflict(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"binding already exists"}`)
	})
	_, err := c.CreateDatabaseBinding(context.Background(), "orders-db", CreateDatabaseBindingRequest{Name: "b"})
	if !IsConflict(err) {
		t.Fatalf("want IsConflict, got %v", err)
	}
}

func TestDeleteDatabaseBinding(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr func(error) bool
	}{
		{"204 succeeds", http.StatusNoContent, nil},
		{"404 is not found", http.StatusNotFound, IsNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodDelete, "/api/v1/tenants/acme/databases/orders-db/bindings/b")
				w.WriteHeader(tt.status)
				if tt.status == http.StatusNotFound {
					fmt.Fprintln(w, `{"error":"binding not found"}`)
				}
			})
			err := c.DeleteDatabaseBinding(context.Background(), "orders-db", "b")
			if tt.wantErr == nil && err != nil {
				t.Fatalf("DeleteDatabaseBinding: %v", err)
			}
			if tt.wantErr != nil && !tt.wantErr(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDatabaseBindingStatus_Conflicted(t *testing.T) {
	tests := []struct {
		name   string
		status *DatabaseBindingStatus
		want   bool
	}{
		{"no status", nil, false},
		{"no conditions", &DatabaseBindingStatus{Phase: "Failed"}, false},
		{"conflict true", &DatabaseBindingStatus{Conditions: []Condition{
			{Type: "Ready", Status: "False", Reason: "SyncConflict"}, {Type: "Conflict", Status: "True", Reason: "SyncConflict"},
		}}, true},
		{"conflict false", &DatabaseBindingStatus{Conditions: []Condition{{Type: "Conflict", Status: "False", Reason: "NoConflict"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Conflicted(); got != tt.want {
				t.Errorf("Conflicted() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseBinding_EffectiveSecretName(t *testing.T) {
	tests := []struct {
		name    string
		binding DatabaseBinding
		want    string
	}{
		{"the platform's name wins", DatabaseBinding{SecretName: "asked", Status: &DatabaseBindingStatus{SecretName: "orders-db-readwrite"}}, "orders-db-readwrite"},
		{"the requested name before the platform reports one", DatabaseBinding{SecretName: "asked", Status: &DatabaseBindingStatus{}}, "asked"},
		{"no status", DatabaseBinding{SecretName: "asked"}, "asked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.binding.EffectiveSecretName(); got != tt.want {
				t.Errorf("EffectiveSecretName() = %q; want %q", got, tt.want)
			}
		})
	}
}

// TestDatabaseBinding_NamespaceStates lists the listed namespaces in order,
// with what the platform reports for each, then any it still reports but no
// longer lists (being withdrawn).
func TestDatabaseBinding_NamespaceStates(t *testing.T) {
	b := DatabaseBinding{
		Namespaces: []string{"jobs", "api", "web"},
		Status: &DatabaseBindingStatus{Namespaces: []DatabaseBindingNamespaceStatus{
			{Name: "api", Phase: "Ready", Host: "orders-db.api.svc.cluster.local"},
			{Name: "old", Phase: "Ready"},
			{Name: "jobs", Phase: "Failed", Message: "conflict"},
		}},
	}
	want := []BindingNamespace{
		{Name: "jobs", Listed: true, Reported: true, Status: DatabaseBindingNamespaceStatus{Name: "jobs", Phase: "Failed", Message: "conflict"}},
		{Name: "api", Listed: true, Reported: true, Status: DatabaseBindingNamespaceStatus{Name: "api", Phase: "Ready", Host: "orders-db.api.svc.cluster.local"}},
		{Name: "web", Listed: true},
		{Name: "old", Reported: true, Status: DatabaseBindingNamespaceStatus{Name: "old", Phase: "Ready"}},
	}
	if got := b.NamespaceStates(); !slices.Equal(got, want) {
		t.Fatalf("NamespaceStates() =\n%+v\nwant\n%+v", got, want)
	}
	unreported := DatabaseBinding{Namespaces: []string{"api"}}
	if got := unreported.NamespaceStates(); !slices.Equal(got, []BindingNamespace{{Name: "api", Listed: true}}) {
		t.Fatalf("without status: %+v", got)
	}
}

// TestDatabase_DecodesRotationStatus pins the status.rotations shape kupe-api
// projects (internal/server/handlers.go rotationStatus): one key per role. A
// role the server did not send reads as not reported, and a server
// predating rotations decodes to nil.
func TestDatabase_DecodesRotationStatus(t *testing.T) {
	const withRotations = `{"name":"orders-db","status":{"phase":"Running",
  "rotations":{"owner":{"requestedAt":"","appliedAt":"","pending":false},
    "readwrite":{"requestedAt":"2026-10-09T12:00:00.123456789Z","appliedAt":"2026-10-01T08:00:00Z","pending":true},
    "readonly":{"requestedAt":"2026-10-09T12:00:00Z","appliedAt":"2026-10-09T12:00:00Z","pending":false,"delivered":true}}}}`
	var db Database
	if err := json.Unmarshal([]byte(withRotations), &db); err != nil {
		t.Fatal(err)
	}
	readwrite, ok := db.Status.Rotations.Role(DatabaseRoleReadWrite)
	if !ok || readwrite.RequestedAt != "2026-10-09T12:00:00.123456789Z" || readwrite.AppliedAt != "2026-10-01T08:00:00Z" || !readwrite.Pending {
		t.Fatalf("readwrite rotation = %+v, reported %v", readwrite, ok)
	}
	readonly, ok := db.Status.Rotations.Role(DatabaseRoleReadOnly)
	if !ok || !readonly.InEffect("2026-10-09T12:00:00Z") {
		t.Fatalf("readonly rotation = %+v, reported %v; want applied and delivered", readonly, ok)
	}
	if owner, ok := db.Status.Rotations.Role(DatabaseRoleOwner); !ok || owner != (RotationStatus{}) {
		t.Fatalf("owner rotation = %+v, reported %v; want reported, never requested", owner, ok)
	}

	const ownerOnly = `{"name":"orders-db","status":{"phase":"Running",
  "rotations":{"owner":{"requestedAt":"","appliedAt":"","pending":false}}}}`
	var partial Database
	if err := json.Unmarshal([]byte(ownerOnly), &partial); err != nil {
		t.Fatal(err)
	}
	if state, ok := partial.Status.Rotations.Role(DatabaseRoleReadOnly); ok {
		t.Fatalf("a role the server did not send must read as not reported, got %+v", state)
	}

	var older Database
	if err := json.Unmarshal([]byte(databaseJSON), &older); err != nil {
		t.Fatal(err)
	}
	if older.Status.Rotations != nil {
		t.Fatalf("a server without rotations should decode to nil, got %+v", older.Status.Rotations)
	}
	if _, ok := older.Status.Rotations.Role(DatabaseRoleOwner); ok {
		t.Fatal("nil rotations must report no role")
	}
}

func TestDatabaseStatus_Ready(t *testing.T) {
	var db Database
	if err := json.Unmarshal([]byte(databaseJSON), &db); err != nil {
		t.Fatal(err)
	}
	if !db.Status.Ready() {
		t.Fatal("a Ready=True condition should read as ready")
	}
	db.Status.Conditions[0].Status = "False"
	if db.Status.Ready() {
		t.Fatal("a Ready=False condition should not read as ready")
	}
	var none *DatabaseStatus
	if none.Ready() {
		t.Fatal("a missing status is not ready")
	}
}

// patchFieldsNotWaitedOn are the PatchDatabaseRequest fields in effect as
// soon as the PATCH lands, so NeedsWait is false for them alone. Every other
// field must make NeedsWait true.
var patchFieldsNotWaitedOn = map[string]bool{"DeletionProtection": true}

// patchFieldsNotInIsEmpty are PatchDatabaseRequest fields IsEmpty may
// ignore. None today: every field sent is a change.
var patchFieldsNotInIsEmpty = map[string]bool{}

// patchWithOnly returns a patch with only field i set to a non-zero value.
// A field kind it does not know fails the test, so a new kind is handled
// here rather than skipped.
func patchWithOnly(t *testing.T, i int) PatchDatabaseRequest {
	t.Helper()
	var p PatchDatabaseRequest
	v := reflect.ValueOf(&p).Elem().Field(i)
	switch v.Kind() {
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	default:
		t.Fatalf("field %s has kind %s: teach patchWithOnly to set it",
			reflect.TypeFor[PatchDatabaseRequest]().Field(i).Name, v.Kind())
	}
	return p
}

// TestPatchDatabaseRequest_IsEmptyCoversEveryField is the guard on the
// update command's "nothing to update" check: a field added to the PATCH
// body but not to IsEmpty would make a patch carrying only that field look
// empty and be refused.
func TestPatchDatabaseRequest_IsEmptyCoversEveryField(t *testing.T) {
	if !(PatchDatabaseRequest{}).IsEmpty() {
		t.Fatal("the zero patch must be empty")
	}
	typ := reflect.TypeFor[PatchDatabaseRequest]()
	for name := range patchFieldsNotInIsEmpty {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("patchFieldsNotInIsEmpty names %q, which PatchDatabaseRequest no longer has", name)
		}
	}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() || patchFieldsNotInIsEmpty[field.Name] {
			continue
		}
		if patchWithOnly(t, i).IsEmpty() {
			t.Errorf("IsEmpty() is true for a patch that sets %s: add it to IsEmpty", field.Name)
		}
	}
}

// TestPatchDatabaseRequest_NeedsWaitCoversEveryField is the guard on the
// update command's wait: a field the platform applies but NeedsWait leaves
// out would return before the change is in effect.
func TestPatchDatabaseRequest_NeedsWaitCoversEveryField(t *testing.T) {
	if (PatchDatabaseRequest{}).NeedsWait() {
		t.Fatal("the zero patch must not be waited on")
	}
	typ := reflect.TypeFor[PatchDatabaseRequest]()
	for name := range patchFieldsNotWaitedOn {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("patchFieldsNotWaitedOn names %q, which PatchDatabaseRequest no longer has", name)
		}
	}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		want := !patchFieldsNotWaitedOn[field.Name]
		if got := patchWithOnly(t, i).NeedsWait(); got != want {
			t.Errorf("NeedsWait() = %v for a patch that sets only %s; want %v", got, field.Name, want)
		}
	}
}

// backupJSON is a representative GET /backups/{backup} body.
const backupJSON = `{"name":"orders-db-20261030-141500","database":"orders-db","reason":"Manual",
  "status":{"phase":"Completed","completedAt":"2026-10-30T14:17:00Z","sizeBytes":1073741824,
    "source":{"name":"orders-db","version":"18","storage":"10Gi"},"databaseDeleted":true,
    "conditions":[{"type":"Ready","status":"True","reason":"Completed","message":"backup completed"}]},
  "resourceVersion":"582031","createdAt":"2026-10-30T14:15:00Z"}`

func TestBackup_Decodes(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/backups/orders-db-20261030-141500")
		fmt.Fprintln(w, backupJSON)
	})
	b, err := c.GetBackup(context.Background(), "orders-db-20261030-141500")
	if err != nil {
		t.Fatalf("GetBackup: %v", err)
	}
	want := Backup{
		Name: "orders-db-20261030-141500", Database: "orders-db", Reason: "Manual",
		Status: &BackupStatus{
			Phase: "Completed", CompletedAt: "2026-10-30T14:17:00Z", SizeBytes: 1073741824,
			Source:          &BackupSource{Name: "orders-db", Version: "18", Storage: "10Gi"},
			DatabaseDeleted: true,
			Conditions:      []Condition{{Type: "Ready", Status: "True", Reason: "Completed", Message: "backup completed"}},
		},
		ResourceVersion: "582031", CreatedAt: "2026-10-30T14:15:00Z",
	}
	if !reflect.DeepEqual(*b, want) {
		t.Fatalf("backup = %+v\nwant %+v", *b, want)
	}
	if got := b.Status.Message(); got != "backup completed" {
		t.Errorf("Message() = %q; want the Ready condition's message", got)
	}
}

func TestListBackups_DatabaseIsAQueryParameter(t *testing.T) {
	tests := []struct {
		name, database, wantQuery string
	}{
		{"every backup of the tenant", "", ""},
		{"the backups of databases with one name", "orders-db", "database=orders-db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/backups")
				if r.URL.RawQuery != tt.wantQuery {
					t.Errorf("query = %q; want %q", r.URL.RawQuery, tt.wantQuery)
				}
				fmt.Fprintf(w, `{"items":[%s]}`, backupJSON)
			})
			list, err := c.ListBackups(context.Background(), tt.database)
			if err != nil {
				t.Fatalf("ListBackups: %v", err)
			}
			if len(list) != 1 || !list[0].Status.DatabaseDeleted {
				t.Fatalf("backups = %+v", list)
			}
		})
	}
}

func TestCreateDatabaseBackup_SendsOnlyAName(t *testing.T) {
	tests := []struct {
		name     string
		req      CreateBackupRequest
		wantBody map[string]any
	}{
		{"no name leaves it to the platform", CreateBackupRequest{}, map[string]any{}},
		{"a name is sent", CreateBackupRequest{Name: "before-migration"}, map[string]any{"name": "before-migration"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases/orders-db/backups")
				if body := decodeBody(t, r); !reflect.DeepEqual(body, tt.wantBody) {
					t.Errorf("body = %v; want %v (never a reason or platform field)", body, tt.wantBody)
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintln(w, `{"name":"before-migration","database":"orders-db","reason":"Manual","status":{"phase":"Pending","databaseDeleted":false}}`)
			})
			b, err := c.CreateDatabaseBackup(context.Background(), "orders-db", tt.req)
			if err != nil {
				t.Fatalf("CreateDatabaseBackup: %v", err)
			}
			if b.Name != "before-migration" || b.Status.Phase != PhasePending {
				t.Fatalf("backup = %+v", b)
			}
		})
	}
}

func TestCreateDatabaseBackup_CodesSurvive(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"code":"backup_limit_reached","severity":"error","message":"database orders-db already has 20 manual backups"}`)
	})
	_, err := c.CreateDatabaseBackup(context.Background(), "orders-db", CreateBackupRequest{})
	if !IsConflict(err) || ErrorCode(err) != BackupCodeLimitReached {
		t.Fatalf("err = %v (code %q); want 409 %s", err, ErrorCode(err), BackupCodeLimitReached)
	}
}

func TestDeleteBackup(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodDelete, "/api/v1/tenants/acme/backups/orders-db-20261030-141500")
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteBackup(context.Background(), "orders-db-20261030-141500"); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
}

func TestCreateDatabase_SendsSource(t *testing.T) {
	tests := []struct {
		name string
		src  *DatabaseSource
		want map[string]any
	}{
		{"from a backup", &DatabaseSource{Backup: "orders-db-20261030-141500"}, map[string]any{"backup": "orders-db-20261030-141500"}},
		{"from a database at its latest point", &DatabaseSource{Database: "orders-db"}, map[string]any{"database": "orders-db"}},
		{"from a database at a point", &DatabaseSource{Database: "orders-db", PointInTime: "2026-10-30T09:00:00Z"},
			map[string]any{"database": "orders-db", "pointInTime": "2026-10-30T09:00:00Z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases")
				body := decodeBody(t, r)
				if !reflect.DeepEqual(body["source"], tt.want) {
					t.Errorf("source = %v; want %v", body["source"], tt.want)
				}
				if _, ok := body["version"]; ok {
					t.Errorf("version sent: %v; a restore leaves it to the platform", body["version"])
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintln(w, `{"name":"orders-copy","source":{"backup":"orders-db-20261030-141500"}}`)
			})
			db, _, err := c.Databases().Create(context.Background(), CreateDatabaseRequest{
				Name: "orders-copy", Resources: &DatabaseResources{CPU: "500m", Memory: "1Gi", Storage: "10Gi"}, Source: tt.src,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if db.Source == nil || db.Source.Backup != "orders-db-20261030-141500" {
				t.Fatalf("source not decoded: %+v", db.Source)
			}
		})
	}
}

func TestIsUnknownRoute(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
	}{
		{"Go's 404 for a route the server lacks", http.NotFound, true},
		{"kupe-api's JSON 404", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, `{"error":"database \"orders-db\" not found"}`)
		}, false},
		{"a coded 404", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, `{"code":"backups_unavailable","message":"backups are not available on this platform"}`)
		}, false},
		{"a 400 with a plain body", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad", http.StatusBadRequest)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, tt.handler)
			_, err := c.ListBackups(context.Background(), "")
			if err == nil {
				t.Fatal("want an error")
			}
			if got := IsUnknownRoute(err); got != tt.want {
				t.Fatalf("IsUnknownRoute = %v; want %v (%v)", got, tt.want, err)
			}
		})
	}
	if IsUnknownRoute(&APIError{StatusCode: 404, Message: "database not found"}) {
		t.Error("a 404 with no body at all is not an unknown route")
	}
}
