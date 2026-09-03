package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newServer is the retry-policy-agnostic sibling of newTestClient for
// clients that aren't the tenant-scoped *Client (tests that need their own retry policy, and the signup client).
func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestDeleteTenantAccepted(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s; want DELETE", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/acme" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer kupe_test" {
			t.Errorf("Authorization = %q", got)
		}
		var body DeleteTenantRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.Confirm != "acme" || !body.Cascade {
			t.Errorf("body = %+v; want confirm=acme cascade=true", body)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, `{"name":"acme","status":{"phase":"Terminating"}}`)
	})
	got, err := c.DeleteTenant(context.Background(), DeleteTenantRequest{Confirm: "acme", Cascade: true})
	if err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if got.Name != "acme" || got.Status == nil || got.Status.Phase != PhaseTerminating {
		t.Fatalf("unexpected 202 body: %+v", got)
	}
}

func TestDeleteTenantOmitsCascadeWhenFalse(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if _, ok := raw["cascade"]; ok {
			t.Errorf("cascade=false should be omitted from the body, got %v", raw)
		}
		if raw["confirm"] != "acme" {
			t.Errorf("confirm = %v", raw["confirm"])
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, `{"name":"acme","status":{"phase":"Terminating"}}`)
	})
	if _, err := c.DeleteTenant(context.Background(), DeleteTenantRequest{Confirm: "acme"}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteTenantErrors covers every status the contract lists, asserting
// both the classifier and (where the server sends one) the canonical code.
func TestDeleteTenantErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		check    func(error) bool
		wantCode string
	}{
		{"400 bad confirm", http.StatusBadRequest, `{"error":"confirm must equal the tenant name"}`, IsValidation, ""},
		{"403 owner_required", http.StatusForbidden, `{"code":"owner_required","message":"only the tenant owner may delete it"}`, IsForbidden, TenantDeleteCodeOwnerRequired},
		{"403 owner_required upper-case code", http.StatusForbidden, `{"code":"OWNER_REQUIRED","message":"api keys cannot delete tenants"}`, IsForbidden, TenantDeleteCodeOwnerRequired},
		{"404 gone", http.StatusNotFound, `{"error":"tenant not found"}`, IsNotFound, ""},
		{"409 clusters_exist", http.StatusConflict, `{"code":"clusters_exist","message":"tenant has 2 active clusters"}`, IsConflict, TenantDeleteCodeClustersExist},
		{"409 already_terminating", http.StatusConflict, `{"code":"already_terminating","message":"tenant is already being deleted"}`, IsConflict, TenantDeleteCodeAlreadyTerminating},
		{"429 rate limited", http.StatusTooManyRequests, `{"error":"rate limited"}`, IsRateLimited, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-Id", "req-del")
				w.WriteHeader(tt.status)
				fmt.Fprintln(w, tt.body)
			})
			// Single attempt: a 429 must not sleep on Retry-After in tests.
			c := New(srv.URL, "acme", "kupe_test", "kupe-cli-test/0.0.0", WithRetryPolicy(retryPolicy{maxAttempts: 1}))
			_, err := c.DeleteTenant(context.Background(), DeleteTenantRequest{Confirm: "acme"})
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.check(err) {
				t.Fatalf("classifier did not match for %d: %v", tt.status, err)
			}
			if got := ErrorCode(err); got != tt.wantCode {
				t.Fatalf("ErrorCode = %q; want %q", got, tt.wantCode)
			}
			if !IsAPIError(err) {
				t.Fatalf("want APIError, got %T", err)
			}
		})
	}
}
