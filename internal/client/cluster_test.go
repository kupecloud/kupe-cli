package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestListClusters(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tenants/acme/clusters" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"items":[{"name":"prod","type":"shared"},{"name":"staging","type":"shared"}]}`)
	})
	list, err := c.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(list) != 2 || list[0].Name != "prod" {
		t.Fatalf("unexpected: %+v", list)
	}
}

func TestCreateClusterConflict(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-conflict")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"cluster already exists"}`)
	})
	_, _, err := c.CreateCluster(context.Background(), CreateClusterRequest{Name: "prod", Type: "shared"})
	if err == nil || !IsConflict(err) {
		t.Fatalf("want IsConflict, got %v", err)
	}
}

func TestUpdateClusterRMWSuccess(t *testing.T) {
	var getHits, patchHits int32

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			atomic.AddInt32(&getHits, 1)
			w.Header().Set("ETag", "42")
			fmt.Fprintln(w, `{"name":"prod","type":"shared","version":"1.32"}`)
		case http.MethodPatch:
			atomic.AddInt32(&patchHits, 1)
			if r.Header.Get("If-Match") != "42" {
				t.Errorf("If-Match = %q; want 42", r.Header.Get("If-Match"))
			}
			var body PatchClusterRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Version == nil || *body.Version != "1.33" {
				t.Errorf("patch body unexpected: %+v", body)
			}
			w.Header().Set("ETag", "43")
			fmt.Fprintln(w, `{"name":"prod","type":"shared","version":"1.33"}`)
		}
	})

	updated, err := c.UpdateClusterRMW(context.Background(), "prod", func(cur *Cluster) *PatchClusterRequest {
		v := "1.33"
		return &PatchClusterRequest{Version: &v}
	})
	if err != nil {
		t.Fatalf("RMW: %v", err)
	}
	if updated.Version != "1.33" {
		t.Fatalf("version = %s", updated.Version)
	}
	if atomic.LoadInt32(&getHits) != 1 || atomic.LoadInt32(&patchHits) != 1 {
		t.Fatalf("attempts: get=%d patch=%d; want 1/1", getHits, patchHits)
	}
}

func TestUpdateClusterRMWRetriesOn412(t *testing.T) {
	var patchHits int32

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("ETag", fmt.Sprintf("%d", atomic.LoadInt32(&patchHits)+1))
			fmt.Fprintln(w, `{"name":"prod","type":"shared","version":"1.32"}`)
		case http.MethodPatch:
			n := atomic.AddInt32(&patchHits, 1)
			if n == 1 {
				w.WriteHeader(http.StatusPreconditionFailed)
				fmt.Fprintln(w, `{"error":"etag mismatch"}`)
				return
			}
			w.Header().Set("ETag", "99")
			fmt.Fprintln(w, `{"name":"prod","version":"1.33"}`)
		}
	})

	updated, err := c.UpdateClusterRMW(context.Background(), "prod", func(*Cluster) *PatchClusterRequest {
		v := "1.33"
		return &PatchClusterRequest{Version: &v}
	})
	if err != nil {
		t.Fatalf("RMW should retry on 412: %v", err)
	}
	if updated.Version != "1.33" {
		t.Fatalf("version after retry = %s", updated.Version)
	}
	if got := atomic.LoadInt32(&patchHits); got != 2 {
		t.Fatalf("PATCH attempts = %d; want 2", got)
	}
}

func TestUpdateClusterRMWFailsAfterTwoMismatches(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("ETag", "1")
			fmt.Fprintln(w, `{"name":"prod"}`)
			return
		}
		w.WriteHeader(http.StatusPreconditionFailed)
		fmt.Fprintln(w, `{"error":"etag mismatch"}`)
	})

	_, err := c.UpdateClusterRMW(context.Background(), "prod", func(*Cluster) *PatchClusterRequest {
		v := "1.33"
		return &PatchClusterRequest{Version: &v}
	})
	if err != ErrRMWContention { //nolint:errorlint
		t.Fatalf("want ErrRMWContention after two 412s, got %v", err)
	}
}

func TestUpdateClusterRMWNoopPatch(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "1")
		fmt.Fprintln(w, `{"name":"prod","version":"1.32"}`)
	})

	updated, err := c.UpdateClusterRMW(context.Background(), "prod", func(*Cluster) *PatchClusterRequest {
		return nil // mutator decided no change needed
	})
	if err != nil {
		t.Fatalf("noop RMW: %v", err)
	}
	if updated.Version != "1.32" {
		t.Fatalf("unexpected noop result: %+v", updated)
	}
}

func TestDeleteClusterNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"cluster not found"}`)
	})
	err := c.DeleteCluster(context.Background(), "ghost")
	if !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}

// Sanity: RMW uses real time but we don't want the retry+backoff to add
// any measurable delay to the test suite.
func TestRMWDoesNotIntroduceBackoff(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("ETag", "1")
			fmt.Fprintln(w, `{"name":"prod","version":"1.32"}`)
			return
		}
		fmt.Fprintln(w, `{"name":"prod","version":"1.33"}`)
	})
	start := time.Now()
	_, err := c.UpdateClusterRMW(context.Background(), "prod", func(*Cluster) *PatchClusterRequest {
		v := "1.33"
		return &PatchClusterRequest{Version: &v}
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("single RMW round-trip took %v; expected sub-200ms", elapsed)
	}
}
