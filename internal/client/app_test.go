package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppEnvironmentOpaqueETagAndConfirmedDelete(t *testing.T) {
	const etag = `"uid.with-dots.9"`
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.EscapedPath() != "/api/v1/tenants/acme/apps/shop/environments/production" {
			t.Errorf("path=%s", r.URL.EscapedPath())
		}
		w.Header().Set("ETag", etag)
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"name":"shop-production","environment":"production"}`))
		case http.MethodPatch:
			if r.Header.Get("If-Match") != etag {
				t.Errorf("patch If-Match=%q", r.Header.Get("If-Match"))
			}
			_, _ = w.Write([]byte(`{"environment":"production"}`))
		case http.MethodDelete:
			if r.Header.Get("If-Match") != etag {
				t.Errorf("delete If-Match=%q", r.Header.Get("If-Match"))
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["confirm"] != "production" {
				t.Errorf("confirm=%v", body)
			}
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "acme", "", "test")
	ctx := context.Background()
	api := c.AppEnvironments("shop")
	_, got, err := api.Get(ctx, "production")
	if err != nil || got != etag {
		t.Fatalf("get etag=%q err=%v", got, err)
	}
	protection := false
	if _, _, err := api.Update(ctx, "production", got, PatchAppEnvironmentRequest{DeletionProtection: &protection}); err != nil {
		t.Fatal(err)
	}
	if err := api.Delete(ctx, "production"); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 4 {
		t.Fatalf("methods=%v", methods)
	}
}

func TestAppUpdateRMWRetriesWithFreshOpaqueETag(t *testing.T) {
	gets, patches := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			w.Header().Set("ETag", fmt.Sprintf(`"uid.%d"`, gets))
			_, _ = w.Write([]byte(`{"name":"shop"}`))
			return
		}
		patches++
		want := fmt.Sprintf(`"uid.%d"`, patches)
		if r.Header.Get("If-Match") != want {
			t.Errorf("If-Match=%q want=%q", r.Header.Get("If-Match"), want)
		}
		if patches == 1 {
			w.WriteHeader(412)
			_, _ = w.Write([]byte(`{"code":"precondition_failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"shop"}`))
	}))
	defer srv.Close()
	p := false
	_, err := New(srv.URL, "acme", "", "test").Apps().UpdateRMW(context.Background(), "shop", func(*App) *PatchAppRequest { return &PatchAppRequest{DeletionProtection: &p} })
	if err != nil || gets != 2 || patches != 2 {
		t.Fatalf("gets=%d patches=%d err=%v", gets, patches, err)
	}
}

func TestAppClientEscapesSegmentsAndRetriesLimiterOnly(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.EscapedPath() != "/api/v1/tenants/acme/apps/shop%2Fblue/environments/prod%2Fwest" {
			t.Errorf("path=%s", r.URL.EscapedPath())
		}
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("ETag", `"uid.1"`)
		_, _ = w.Write([]byte(`{"environment":"prod/west"}`))
	}))
	defer srv.Close()
	_, _, err := New(srv.URL, "acme", "", "test", WithRetryPolicy(retryPolicy{maxAttempts: 2})).AppEnvironments("shop/blue").Get(context.Background(), "prod/west")
	if err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestAppClientCollectionPathsAndCreateBodies(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprint(child), func(t *testing.T) {
			path := "/api/v1/tenants/acme/apps"
			if child {
				path += "/shop/environments"
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.EscapedPath() != path {
					t.Errorf("path=%s want=%s", r.URL.EscapedPath(), path)
				}
				w.Header().Set("ETag", `"created.1"`)
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"items":[]}`))
					return
				}
				if r.Method != http.MethodPost {
					t.Errorf("method=%s", r.Method)
				}
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				key, name := "name", "shop"
				if child {
					key, name = "environment", "production"
				}
				if req[key] != name || req["deletionProtection"] != false {
					t.Errorf("body=%v", req)
				}
				w.WriteHeader(201)
				_, _ = w.Write([]byte(`{"name":"created"}`))
			}))
			defer srv.Close()
			c := New(srv.URL, "acme", "", "test")
			p := false
			ctx := context.Background()
			if child {
				if _, err := c.AppEnvironments("shop").List(ctx); err != nil {
					t.Fatal(err)
				}
				_, etag, err := c.AppEnvironments("shop").Create(ctx, CreateAppEnvironmentRequest{Environment: "production", Cluster: "europe", DeletionProtection: &p})
				if err != nil || etag != `"created.1"` {
					t.Fatalf("etag=%q err=%v", etag, err)
				}
			} else {
				if _, err := c.Apps().List(ctx); err != nil {
					t.Fatal(err)
				}
				_, etag, err := c.Apps().Create(ctx, CreateAppRequest{Name: "shop", DeletionProtection: &p})
				if err != nil || etag != `"created.1"` {
					t.Fatalf("etag=%q err=%v", etag, err)
				}
			}
			if calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
