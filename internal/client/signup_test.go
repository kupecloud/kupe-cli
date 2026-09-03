package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestDeleteUserNoContent(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s; want DELETE", r.Method)
		}
		if r.URL.Path != "/users/me" {
			t.Errorf("path = %s; want /users/me", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oidc-jwt" {
			t.Errorf("Authorization = %q", got)
		}
		var body DeleteUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.Confirm != "billy@acme.com" {
			t.Errorf("confirm = %q", body.Confirm)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s := NewSignup(srv.URL, "oidc-jwt", "kupe-cli-test/0.0.0", WithRetryPolicy(fastRetry))
	if err := s.DeleteUser(context.Background(), DeleteUserRequest{Confirm: "billy@acme.com"}); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
}

func TestDeleteUserUsesTokenSource(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Errorf("Authorization = %q; want token source value", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s := NewSignup(srv.URL, "", "kupe-cli-test/0.0.0", WithTokenSource(func(context.Context) (string, error) { return "fresh", nil }))
	if err := s.DeleteUser(context.Background(), DeleteUserRequest{Confirm: "x@y"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteUserErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
	}{
		{"400 bad confirm", http.StatusBadRequest, `{"error":"confirm must equal the token email"}`, IsValidation},
		{"403 api key", http.StatusForbidden, `{"error":"OIDC token required"}`, IsForbidden},
		{"429 rate limited", http.StatusTooManyRequests, `{"error":"rate limited"}`, IsRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-Id", "req-user")
				w.WriteHeader(tt.status)
				fmt.Fprintln(w, tt.body)
			})
			s := NewSignup(srv.URL, "jwt", "kupe-cli-test/0.0.0", WithRetryPolicy(retryPolicy{maxAttempts: 1}))
			err := s.DeleteUser(context.Background(), DeleteUserRequest{Confirm: "x@y"})
			if err == nil {
				t.Fatal("want error")
			}
			if !tt.check(err) {
				t.Fatalf("classifier did not match for %d: %v", tt.status, err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.RequestID != "req-user" {
				t.Fatalf("request-id not propagated: %v", err)
			}
		})
	}
}

func TestDeleteUserTenantMembershipsConflict(t *testing.T) {
	bodies := map[string]string{
		"string list":  `{"code":"tenant_memberships","message":"leave your tenants first","tenants":["acme","beta"]}`,
		"object list":  `{"code":"tenant_memberships","message":"leave your tenants first","tenants":[{"name":"acme"},{"name":"beta"}]}`,
		"under detail": `{"code":"tenant_memberships","message":"leave your tenants first","details":{"tenants":["acme","beta"]}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprintln(w, body)
			})
			s := NewSignup(srv.URL, "jwt", "kupe-cli-test/0.0.0", WithRetryPolicy(fastRetry))
			err := s.DeleteUser(context.Background(), DeleteUserRequest{Confirm: "x@y"})
			var me *TenantMembershipsError
			if !errors.As(err, &me) {
				t.Fatalf("want *TenantMembershipsError, got %T: %v", err, err)
			}
			if len(me.Tenants) != 2 || me.Tenants[0] != "acme" || me.Tenants[1] != "beta" {
				t.Fatalf("tenants = %v", me.Tenants)
			}
			if !IsConflict(err) {
				t.Fatalf("IsConflict must see through the wrapper: %v", err)
			}
			if ErrorCode(err) != UserDeleteCodeTenantMemberships {
				t.Fatalf("ErrorCode = %q", ErrorCode(err))
			}
		})
	}
}

func TestDeleteUserConflictWithoutTenantListKeepsMessage(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"you still belong to tenant acme"}`)
	})
	s := NewSignup(srv.URL, "jwt", "kupe-cli-test/0.0.0", WithRetryPolicy(fastRetry))
	err := s.DeleteUser(context.Background(), DeleteUserRequest{Confirm: "x@y"})
	var me *TenantMembershipsError
	if !errors.As(err, &me) || len(me.Tenants) != 0 {
		t.Fatalf("want wrapper with no tenants, got %T %v", err, err)
	}
	if err.Error() != "you still belong to tenant acme" {
		t.Fatalf("Error() = %q; want the server message", err.Error())
	}
}
