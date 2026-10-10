package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	list, err := c.ListDatabases(context.Background())
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
	db, etag, err := c.GetDatabase(context.Background(), "orders-db")
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if etag != "582019" || db.Version != "18" {
		t.Fatalf("got etag=%q db=%+v", etag, db)
	}
}

func TestCreateDatabase_SendsOnlySetFields(t *testing.T) {
	off := false
	tests := []struct {
		name    string
		req     CreateDatabaseRequest
		want    map[string]bool // field → present in body
		wantDP  any
		wantVer any
	}{
		{
			name: "defaults leave version, HA and deletion protection to the server",
			req:  CreateDatabaseRequest{Name: "orders-db", Engine: "postgres", Resources: &DatabaseResources{CPU: "500m", Memory: "1Gi", Storage: "10Gi"}},
			want: map[string]bool{"version": false, "highAvailability": false, "deletionProtection": false},
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
				w.Header().Set("ETag", "1")
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintln(w, databaseJSON)
			})
			db, etag, err := c.CreateDatabase(context.Background(), tt.req)
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
	_, _, err := c.CreateDatabase(context.Background(), CreateDatabaseRequest{Name: "orders-db"})
	if !IsConflict(err) {
		t.Fatalf("want IsConflict, got %v", err)
	}
}

func TestUpdateDatabase_SendsIfMatchAndOnlySetFields(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPatch, "/api/v1/tenants/acme/databases/orders-db")
		if got := r.Header.Get("If-Match"); got != "7" {
			t.Errorf("If-Match = %q; want 7", got)
		}
		body := decodeBody(t, r)
		if len(body) != 1 || body["deletionProtection"] != false {
			t.Errorf("body = %v; want only deletionProtection=false", body)
		}
		w.Header().Set("ETag", "8")
		fmt.Fprintln(w, databaseJSON)
	})
	off := false
	_, etag, err := c.UpdateDatabase(context.Background(), "orders-db", "7", PatchDatabaseRequest{DeletionProtection: &off})
	if err != nil {
		t.Fatalf("UpdateDatabase: %v", err)
	}
	if etag != "8" {
		t.Fatalf("etag = %q; want 8", etag)
	}
}

func TestUpdateDatabaseRMW(t *testing.T) {
	v18 := "18"
	tests := []struct {
		name        string
		patchStatus []int // response status per PATCH attempt
		mutate      DatabaseMutator
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
			db, err := c.UpdateDatabaseRMW(context.Background(), "orders-db", tt.mutate)
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
			err := c.DeleteDatabase(context.Background(), "orders-db")
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
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases/orders-db/rotate-credentials")
		if body := decodeBody(t, r); len(body) != 1 || body["role"] != "app" {
			t.Errorf("body = %v; want {role: app}", body)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, `{"database":"orders-db","role":"app","requestedAt":"2026-10-09T12:00:00Z"}`)
	})
	rot, err := c.RotateDatabaseCredentials(context.Background(), "orders-db", DatabaseRoleApp)
	if err != nil {
		t.Fatalf("RotateDatabaseCredentials: %v", err)
	}
	if rot.Database != "orders-db" || rot.Role != "app" || rot.RequestedAt == "" {
		t.Fatalf("rotation = %+v", rot)
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

const bindingJSON = `{"name":"orders-db-api-app","database":"orders-db","cluster":"prod","namespace":"api","role":"app",
  "secretName":"","status":{"phase":"Ready","host":"orders-db.api.svc.cluster.local","secretName":"orders-db-app"},
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
	if len(list) != 1 || list[0].Status.Host != "orders-db.api.svc.cluster.local" || list[0].Status.SecretName != "orders-db-app" {
		t.Fatalf("decoded bindings = %+v", list)
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
		expectRequest(t, r, http.MethodGet, "/api/v1/tenants/acme/databases/orders-db/bindings/orders-db-api-app")
		fmt.Fprintln(w, bindingJSON)
	})
	b, err := c.GetDatabaseBinding(context.Background(), "orders-db", "orders-db-api-app")
	if err != nil {
		t.Fatalf("GetDatabaseBinding: %v", err)
	}
	if b.Cluster != "prod" || b.Role != "app" {
		t.Fatalf("binding = %+v", b)
	}
}

func TestCreateDatabaseBinding(t *testing.T) {
	tests := []struct {
		name           string
		req            CreateDatabaseBindingRequest
		wantSecretSent bool
	}{
		{"secret name omitted when empty", CreateDatabaseBindingRequest{Name: "b", Cluster: "prod", Namespace: "api", Role: "app"}, false},
		{"secret name sent when set", CreateDatabaseBindingRequest{Name: "b", Cluster: "prod", Namespace: "api", Role: "app", SecretName: "pg"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases/orders-db/bindings")
				body := decodeBody(t, r)
				if _, ok := body["secretName"]; ok != tt.wantSecretSent {
					t.Errorf("secretName present=%v; want %v", ok, tt.wantSecretSent)
				}
				if body["cluster"] != "prod" || body["namespace"] != "api" || body["role"] != "app" || body["name"] != "b" {
					t.Errorf("body = %v", body)
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
		{"the platform's name wins", DatabaseBinding{SecretName: "asked", Status: &DatabaseBindingStatus{SecretName: "orders-db-app"}}, "orders-db-app"},
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
