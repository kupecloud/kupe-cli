package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestAccessPostgresListsWire(t *testing.T) {
	roles := []DeclaredRole{{Name: "reporting"}}
	empty := []Extension{}
	for _, tt := range []struct {
		name  string
		patch PatchDatabaseRequest
		want  string
	}{
		{"roles only", PatchDatabaseRequest{Postgres: &DatabasePostgres{Roles: &roles}}, `{"postgres":{"roles":[{"name":"reporting"}]}}`},
		{"clear extensions", PatchDatabaseRequest{Postgres: &DatabasePostgres{Extensions: &empty}}, `{"postgres":{"extensions":[]}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.patch)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tt.want {
				t.Fatalf("body=%s want=%s", body, tt.want)
			}
		})
	}
}

func TestAccessBindingPatchWire(t *testing.T) {
	for _, tt := range []struct {
		name  string
		patch PatchDatabaseBindingRequest
		want  string
	}{
		{"credentials only", PatchDatabaseBindingRequest{Credentials: &BindingCredentials{Dedicated: true}}, `{"credentials":{"dedicated":true}}`},
		{"namespaces only", PatchDatabaseBindingRequest{Namespaces: []string{"orders-api"}}, `{"namespaces":["orders-api"]}`},
		{"both", PatchDatabaseBindingRequest{Credentials: &BindingCredentials{Dedicated: true}, Namespaces: []string{"orders-api"}}, `{"credentials":{"dedicated":true},"namespaces":["orders-api"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.Header().Set("ETag", "revision")
					fmt.Fprint(w, `{"name":"b","namespaces":["orders-api"]}`)
				case http.MethodPatch:
					if r.Header.Get("If-Match") != "revision" {
						t.Error("missing If-Match")
					}
					got := decodeBody(t, r)
					var want map[string]any
					if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body=%v want=%v", got, want)
					}
					fmt.Fprint(w, `{"name":"b"}`)
				default:
					t.Errorf("unexpected %s", r.Method)
				}
			})
			_, err := c.UpdateDatabaseBindingRMW(context.Background(), "orders-db", "b", func(*DatabaseBinding) *PatchDatabaseBindingRequest { return &tt.patch })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAccessBindingRotationRoute(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/api/v1/tenants/acme/databases/orders-db/bindings/reporting/rotate-credentials")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"database":"orders-db","binding":"reporting","requestedAt":"opaque"}`)
	})
	got, err := c.RotateDatabaseBindingCredentials(context.Background(), "orders-db", "reporting")
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestedAt != "opaque" || got.Binding != "reporting" {
		t.Fatalf("got=%+v", got)
	}
}

func TestAccessPostgresRMWRetriesLists(t *testing.T) {
	reads, patches := 0, 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reads++
			w.Header().Set("ETag", fmt.Sprint(reads))
			if reads == 1 {
				fmt.Fprint(w, `{"name":"orders-db","postgres":{"roles":[{"name":"a"}]}}`)
			} else {
				fmt.Fprint(w, `{"name":"orders-db","postgres":{"roles":[{"name":"a"},{"name":"concurrent"}]}}`)
			}
			return
		}
		patches++
		body := decodeBody(t, r)
		roles := body["postgres"].(map[string]any)["roles"].([]any)
		if patches == 1 {
			w.WriteHeader(http.StatusPreconditionFailed)
			fmt.Fprint(w, `{"error":"conflict"}`)
			return
		}
		if len(roles) != 3 || r.Header.Get("If-Match") != "2" {
			t.Errorf("retry lost concurrent edit: %v, If-Match=%s", body, r.Header.Get("If-Match"))
		}
		fmt.Fprint(w, `{"name":"orders-db"}`)
	})
	_, err := c.Databases().UpdateRMW(context.Background(), "orders-db", func(db *Database) *PatchDatabaseRequest {
		roles := append([]DeclaredRole{}, (*db.Postgres.Roles)...)
		roles = append(roles, DeclaredRole{Name: "added"})
		return &PatchDatabaseRequest{Postgres: &DatabasePostgres{Roles: &roles}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 || patches != 2 {
		t.Fatalf("reads=%d patches=%d", reads, patches)
	}
}
