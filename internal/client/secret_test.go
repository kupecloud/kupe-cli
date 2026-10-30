package client

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestListSecrets(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tenants/acme/secrets" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"items":[
		  {"name":"a","secretPath":"kv/a"},
		  {"name":"b","secretPath":"kv/b"}
		]}`)
	})
	list, err := c.ListSecrets(context.Background())
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(list) != 2 || list[0].Name != "a" {
		t.Fatalf("unexpected: %+v", list)
	}
}

func TestCreateSecretConflict(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "req-409")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"secret already exists"}`)
	})
	_, _, err := c.CreateSecret(context.Background(), CreateSecretRequest{Name: "x", SecretPath: "kv/x"})
	if !IsConflict(err) {
		t.Fatalf("want IsConflict, got %v", err)
	}
}

func TestUpdateSecretRMWSuccess(t *testing.T) {
	var patchHits int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("ETag", "10")
			fmt.Fprintln(w, `{"name":"x","secretPath":"kv/x","sync":[]}`)
		case http.MethodPatch:
			atomic.AddInt32(&patchHits, 1)
			if r.Header.Get("If-Match") != "10" {
				t.Errorf("If-Match = %q; want 10", r.Header.Get("If-Match"))
			}
			w.Header().Set("ETag", "11")
			fmt.Fprintln(w, `{"name":"x","secretPath":"kv/x","sync":[{"cluster":"prod","namespace":"default"}]}`)
		}
	})
	updated, err := c.UpdateSecretRMW(context.Background(), "x", func(_ *Secret) *PatchSecretRequest {
		return &PatchSecretRequest{Sync: []SyncTarget{{Cluster: "prod", Namespace: "default"}}}
	})
	if err != nil {
		t.Fatalf("RMW: %v", err)
	}
	if len(updated.Sync) != 1 || updated.Sync[0].Cluster != "prod" {
		t.Fatalf("unexpected: %+v", updated)
	}
	if got := atomic.LoadInt32(&patchHits); got != 1 {
		t.Fatalf("PATCH attempts = %d; want 1", got)
	}
}

func TestUpdateSecretRMW412RetriesOnce(t *testing.T) {
	var patchHits int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("ETag", "1")
			fmt.Fprintln(w, `{"name":"x","secretPath":"kv/x"}`)
			return
		}
		n := atomic.AddInt32(&patchHits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusPreconditionFailed)
			fmt.Fprintln(w, `{"error":"etag mismatch"}`)
			return
		}
		fmt.Fprintln(w, `{"name":"x"}`)
	})
	_, err := c.UpdateSecretRMW(context.Background(), "x", func(*Secret) *PatchSecretRequest {
		return &PatchSecretRequest{Sync: nil}
	})
	if err != nil {
		t.Fatalf("RMW should retry on 412: %v", err)
	}
	if got := atomic.LoadInt32(&patchHits); got != 2 {
		t.Fatalf("PATCH attempts = %d; want 2", got)
	}
}

func TestDeleteSecretNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"secret not found"}`)
	})
	err := c.DeleteSecret(context.Background(), "ghost")
	if !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}

func TestRefreshSecretPostsAndDecodesToken(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tenants/acme/secrets/db/refresh" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, `{"name":"db","requestedAt":"opaque-token"}`)
	})
	got, err := c.RefreshSecret(context.Background(), "db")
	if err != nil || got != "opaque-token" {
		t.Fatalf("token=%q err=%v", got, err)
	}
}

func TestSecretStatusDecode(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"name":"db","generation":3,"sync":[{"cluster":"c","namespace":"ns","keys":["API_KEY"]}],"status":{"phase":"Active","observedGeneration":3,"observedRefresh":"token","refresh":{"requestedAt":"token","observedAt":"token","pending":false},"conditions":[{"type":"Ready","status":"True"}],"syncStatuses":[{"cluster":"c","namespace":"ns","secretName":"db","phase":"Synced","reason":"Delivered","deliveredRefresh":"token","lastDeliveredAt":"2026-10-30T12:00:00Z","message":"delivered"}]}}`)
	})
	s, _, err := c.GetSecret(context.Background(), "db")
	if err != nil {
		t.Fatal(err)
	}
	if s.Generation != 3 || s.Sync[0].Keys[0] != "API_KEY" || s.Status.ObservedGeneration != 3 || s.Status.ObservedRefresh != "token" || s.Status.Refresh.ObservedAt != "token" || s.Status.Conditions[0].Status != "True" || s.Status.SyncStatuses[0].DeliveredRefresh != "token" || s.Status.SyncStatuses[0].LastDeliveredAt == "" || s.Status.SyncStatuses[0].Message != "delivered" {
		t.Fatalf("decode=%+v", s)
	}
}

func TestRefreshSecretReturnsTerminatingCode(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"code":"terminating","message":"secret is being deleted"}`)
	})
	if _, err := c.RefreshSecret(context.Background(), "db"); !IsConflict(err) || ErrorCode(err) != "terminating" {
		t.Fatalf("err=%v", err)
	}
}
