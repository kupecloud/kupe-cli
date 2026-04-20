package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestListAPIKeys(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tenants/acme/apikeys" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"items":[
		  {"id":"a","displayName":"one","role":"admin","createdBy":"x","createdAt":"2026-01-01T00:00:00Z"},
		  {"id":"b","displayName":"two","role":"readonly","createdBy":"x","createdAt":"2026-01-01T00:00:00Z"}
		]}`)
	})
	keys, err := c.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("want 2 keys, got %d", len(keys))
	}
	if keys[0].Key != "" || keys[1].Key != "" {
		t.Fatalf("raw key leaked on list: %+v", keys)
	}
}

func TestCreateAPIKeyReturnsRawToken(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		var body CreateAPIKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.DisplayName != "CI" || body.Role != "admin" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, `{
		  "id":"abc123",
		  "displayName":"CI",
		  "role":"admin",
		  "createdBy":"x",
		  "createdAt":"2026-04-20T00:00:00Z",
		  "key":"kupe_abc123_secret"
		}`)
	})

	k, err := c.CreateAPIKey(context.Background(), CreateAPIKeyRequest{DisplayName: "CI", Role: "admin"})
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if k.Key != "kupe_abc123_secret" {
		t.Fatalf("raw key missing: %+v", k)
	}
}

func TestCreateAPIKeyForbiddenMapsToAuth(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "req-403")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintln(w, `{"error":"admin access required"}`)
	})
	_, err := c.CreateAPIKey(context.Background(), CreateAPIKeyRequest{DisplayName: "x", Role: "admin"})
	if !IsForbidden(err) {
		t.Fatalf("want IsForbidden, got %v", err)
	}
}

func TestDeleteAPIKeyNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"api key not found"}`)
	})
	err := c.DeleteAPIKey(context.Background(), "ghost")
	if !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}

// Sanity: POST on apikeys should NOT retry even on 503 — duplicate-creation
// risk.
func TestCreateAPIKeyNoRetryOn503(t *testing.T) {
	hits := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := c.CreateAPIKey(context.Background(), CreateAPIKeyRequest{DisplayName: "x", Role: "admin"})
	if err == nil {
		t.Fatal("expected 503 error")
	}
	if hits != 1 {
		t.Fatalf("POST retried %d times; expected 1", hits)
	}
}
