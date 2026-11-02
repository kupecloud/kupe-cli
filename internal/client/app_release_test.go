package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppReleaseEndpointsUsePublishedWireShape(t *testing.T) {
	const match = `"uid.with-dots.17"`
	var calls []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		switch {
		case strings.HasSuffix(r.URL.Path, "/retry"):
			if r.Method != "POST" || r.Header.Get("If-Match") != match {
				t.Errorf("retry method=%s precondition=%q", r.Method, r.Header.Get("If-Match"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || len(body) != 0 {
				t.Errorf("retry must have no body: %q %v", body, err)
			}
			w.Header().Set("ETag", `"uid.with-dots.18"`)
			w.WriteHeader(202)
			fmt.Fprint(w, `{"app":"shop/blue","environment":"prod/west","deploymentRevision":3,"generation":18}`)
		case strings.HasSuffix(r.URL.Path, "/releases"):
			if r.Method != "GET" {
				t.Error(r.Method)
			}
			fmt.Fprint(w, `{"items":[{"name":"shop-production-7","number":7,"deploymentRevision":3,"trigger":"Retry","runtime":{"image":"registry.example/team/app@sha256:abc","port":8080,"env":[],"secrets":[{"name":"db","path":"apps/db","keys":["PASSWORD"]}]},"chart":{"revision":"chart-commit"},"status":{"phase":"Failed","imageDigest":"sha256:abc","recovery":{"phase":"Succeeded","release":{"name":"shop-production-6"}}}}]}`)
		case strings.HasSuffix(r.URL.Path, "/app-images/resolve"):
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 || body["image"] != "nginx:1.27" {
				t.Errorf("body=%v err=%v", body, err)
			}
			fmt.Fprint(w, `{"reference":"registry.example/team/app@sha256:abc","platforms":[{"os":"linux","architecture":"amd64","variant":"","digest":"sha256:abc"}],"user":"101","exposedPorts":[8080]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.EscapedPath())
		}
	})
	ctx := context.Background()
	accepted, etag, err := c.RetryAppEnvironment(ctx, "shop/blue", "prod/west", match)
	if err != nil || etag != `"uid.with-dots.18"` || accepted.Generation != 18 || accepted.DeploymentRevision != 3 {
		t.Fatalf("retry=%+v etag=%q err=%v", accepted, etag, err)
	}
	items, err := c.ListAppReleases(ctx, "shop/blue", "prod/west")
	if err != nil || len(items) != 1 || items[0].Runtime.Image != "registry.example/team/app@sha256:abc" || items[0].Runtime.Secrets[0].Keys[0] != "PASSWORD" || items[0].Status.Recovery.Phase != "Succeeded" {
		t.Fatalf("releases=%+v err=%v", items, err)
	}
	encoded, err := json.Marshal(items[0].Runtime)
	if err != nil || strings.Contains(string(encoded), `"web"`) || !strings.Contains(string(encoded), `"image"`) {
		t.Fatalf("runtime=%s err=%v", encoded, err)
	}
	image, err := c.ResolveAppImage(ctx, "nginx:1.27")
	if err != nil || image.User != "101" || image.Platforms[0].Architecture != "amd64" || image.ExposedPorts[0] != 8080 {
		t.Fatalf("image=%+v err=%v", image, err)
	}
	want := []string{"POST /api/v1/tenants/acme/apps/shop%2Fblue/environments/prod%2Fwest/retry", "GET /api/v1/tenants/acme/apps/shop%2Fblue/environments/prod%2Fwest/releases", "POST /api/v1/tenants/acme/app-images/resolve"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls=%v", calls)
	}
}

func TestAppRetryNeverReplaysUncertainOutcomeOrRedirect(t *testing.T) {
	for _, failure := range []string{"503", "reset", "timeout", "truncated", "truncated429", "307", "308"} {
		t.Run(failure, func(t *testing.T) {
			var calls atomic.Int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				switch failure {
				case "503":
					w.WriteHeader(503)
					fmt.Fprint(w, "synthetic-ingress-body")
				case "reset":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				case "timeout":
					<-r.Context().Done()
				case "truncated":
					w.Header().Set("Content-Length", "100")
					fmt.Fprint(w, `{"app":`)
				case "truncated429":
					w.Header().Set("Content-Length", "100")
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":`)
				case "307", "308":
					w.Header().Set("Location", "/replayed?synthetic=private")
					if failure == "307" {
						w.WriteHeader(307)
					} else {
						w.WriteHeader(308)
					}
				}
			})
			if failure == "timeout" {
				c.httpClient.Timeout = 20 * time.Millisecond
			}
			_, _, err := c.RetryAppEnvironment(context.Background(), "shop", "production", `"uid.1"`)
			var notSent *RequestNotSentError
			if err == nil || calls.Load() != 1 || errors.As(err, &notSent) {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestAppRetryLimiterRefusalAndUnsentSetup(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	})
	_, _, err := c.RetryAppEnvironment(context.Background(), "shop", "production", `"uid.1"`)
	if !IsRateLimited(err) || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	c.tokenSource = func(context.Context) (string, error) { return "", errors.New("synthetic-auth-output") }
	_, _, err = c.RetryAppEnvironment(context.Background(), "shop", "production", `"uid.1"`)
	var setup *RequestNotSentError
	if !errors.As(err, &setup) || !setup.TokenResolution || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestImageResolutionErrorsPreserveSafeRetryHint(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{{"image_not_found", 404}, {"image_unavailable", 503}, {"image_platform_unsupported", 400}, {"image_policy_denied", 400}} {
		t.Run(tc.code, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-Request-Id", "image-request")
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"code":%q,"message":"refused"}`, tc.code)
			})
			_, err := c.ResolveAppImage(context.Background(), "nginx:1.27")
			var api *APIError
			if !errors.As(err, &api) || api.Code != tc.code || api.RetryAfter != time.Minute || api.RequestID != "image-request" {
				t.Fatalf("err=%#v", err)
			}
		})
	}
}

func TestImageBudgetRetryHintPreservesHourAndRejectsInvalidHeaders(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{{"3600", time.Hour}, {"synthetic-private-header", 0}, {"9223372036854775807", 0}, {"-1", 0}} {
		t.Run(tc.header, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", tc.header)
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":"image_unavailable"}`)
			})
			_, err := c.ResolveAppImage(context.Background(), "image:tag")
			var api *APIError
			if !errors.As(err, &api) || api.RetryAfter != tc.want {
				t.Fatalf("err=%v hint=%v", err, api)
			}
		})
	}
}
