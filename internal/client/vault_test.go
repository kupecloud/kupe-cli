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

func TestVaultPreconditionsAndEscaping(t *testing.T) {
	for _, version := range []int64{0, 7} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "PUT" || r.URL.EscapedPath() != "/api/v1/tenants/acme/vault/team/a%20b%25%3F" {
					t.Errorf("request %s %s", r.Method, r.URL.EscapedPath())
				}
				if r.Header.Get("Authorization") != "Bearer kupe_test" {
					t.Error("missing auth")
				}
				match, none := r.Header.Get("If-Match"), r.Header.Get("If-None-Match")
				if version == 0 && (none != "*" || match != "") || version > 0 && (match != `"7"` || none != "") {
					t.Errorf("preconditions %q %q", match, none)
				}
				var body struct {
					Values map[string]string `json:"values"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Values["FIELD_A"] != "synthetic-value" {
					t.Error("bad body")
				}
				fmt.Fprint(w, `{"path":"team/a b%?","version":8,"refreshRequested":["orders"]}`)
			})
			got, err := c.PutVaultValues(context.Background(), "team/a b%?", map[string]string{"FIELD_A": "synthetic-value"}, version)
			if err != nil || got.Version != 8 || len(got.RefreshRequested) != 1 {
				t.Fatalf("result %+v %v", got, err)
			}
		})
	}
}

func TestVaultMetadataAndPurge(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.RawQuery != "" {
			t.Error("metadata request has query")
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `{"path":"team/db","version":3,"updatedAt":"now","deleted":false,"references":["orders"],"values":{"FIELD_A":"do-not-decode"}}`)
			return
		}
		if r.Header.Get("If-Match") != `"3"` {
			t.Error("missing purge version")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["confirm"] != "team/db" {
			t.Error("bad confirmation")
		}
		w.WriteHeader(204)
	})
	got, err := c.GetVaultEntry(context.Background(), "team/db")
	if err != nil || got.Version != 3 || len(got.References) != 1 {
		t.Fatalf("metadata %+v %v", got, err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "do-not-decode") || strings.Contains(string(encoded), `"values"`) || string(encoded) == "" {
		t.Fatal("missing metadata")
	}
	if err := c.PurgeVaultEntry(context.Background(), "team/db", 3); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestVaultTypedErrors(t *testing.T) {
	for code, status := range map[string]int{"secret_version_conflict": 412, "precondition_required": 428, "invalid_secret_values": 400, "secret_write_unconfirmed": 503, "secret_references_exist": 409, "secret_path_reserved": 403, "vault_not_configured": 404, "vault_unavailable": 503, "too_large": 413} {
		t.Run(code, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "vault-test")
				w.WriteHeader(status)
				fmt.Fprintf(w, `{"code":%q,"message":"refused"}`, code)
			})
			_, err := c.PutVaultValues(context.Background(), "db", nil, 1)
			var ae *APIError
			if !errors.As(err, &ae) || ae.StatusCode != status || ae.Code != code || ae.RequestID != "vault-test" {
				t.Fatalf("error %#v", err)
			}
		})
	}
}

func TestVaultWritesNeverReplayUnknownOutcome(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		for _, failure := range []string{"503", "reset", "timeout"} {
			t.Run(method+failure, func(t *testing.T) {
				var calls atomic.Int32
				c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					switch failure {
					case "503":
						w.WriteHeader(503)
					case "reset":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
					case "timeout":
						<-r.Context().Done()
					}
				})
				if failure == "timeout" {
					c.httpClient.Timeout = 20 * time.Millisecond
				}
				var err error
				if method == "PUT" {
					_, err = c.PutVaultValues(context.Background(), "db", map[string]string{"A": "synthetic"}, 1)
				} else {
					err = c.PurgeVaultEntry(context.Background(), "db", 1)
				}
				if err == nil || calls.Load() != 1 {
					t.Fatalf("attempts=%d err=%v", calls.Load(), err)
				}
			})
		}
	}
}

func TestVaultWritesRetry429Once(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
			})
			if method == "PUT" {
				_, _ = c.PutVaultValues(context.Background(), "db", nil, 1)
			} else {
				_ = c.PurgeVaultEntry(context.Background(), "db", 1)
			}
			if calls != 2 {
				t.Fatalf("attempts=%d", calls)
			}
		})
	}
}

func TestVaultWritesRefuseRedirects(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, operation := range []string{"create", "update", "purge"} {
			t.Run(fmt.Sprintf("%s/%d", operation, status), func(t *testing.T) {
				const sentinel = "synthetic-redirect-value"
				initial, target := 0, 0
				c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/redirected" {
						target++
						if operation == "purge" {
							w.WriteHeader(204)
						} else {
							fmt.Fprint(w, `{"path":"db","version":2,"refreshRequested":[]}`)
						}
						return
					}
					initial++
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Location", "/redirected?value="+sentinel)
					w.Header().Set("X-Request-Id", "redirect-test")
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"message":%q}`, sentinel)
				})
				var err error
				if operation == "purge" {
					err = c.PurgeVaultEntry(context.Background(), "db", 1)
				} else {
					version := int64(1)
					if operation == "create" {
						version = 0
					}
					_, err = c.PutVaultValues(context.Background(), "db", map[string]string{"FIELD_A": sentinel}, version)
				}
				if initial != 1 || target != 0 || err == nil {
					t.Fatalf("initial=%d target=%d err=%v", initial, target, err)
				}
				var ae *APIError
				if !errors.As(err, &ae) || ae.StatusCode != status || ae.RequestID != "redirect-test" || ae.Message != http.StatusText(status) || len(ae.Body()) != 0 {
					t.Fatalf("unsafe or untyped redirect error: %#v", err)
				}
				if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "/redirected") {
					t.Fatalf("redirect data in error: %v", err)
				}
			})
		}
	}
}

func TestVaultRedirectRefusalPreservesLegacyPolicy(t *testing.T) {
	redirects, target := 0, 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			target++
			fmt.Fprint(w, `{"name":"acme"}`)
			return
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	original := c.httpClient
	original.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { redirects++; return nil }
	_, err := c.PutVaultValues(context.Background(), "db", map[string]string{"A": "synthetic"}, 1)
	if err == nil || redirects != 0 || target != 0 || c.httpClient != original {
		t.Fatalf("Vault redirect followed or shared client mutated: redirects=%d target=%d err=%v", redirects, target, err)
	}
	tenant, _, err := c.GetTenant(context.Background())
	if err != nil || tenant.Name != "acme" || redirects != 1 || target != 1 {
		t.Fatalf("legacy redirect changed: redirects=%d target=%d tenant=%+v err=%v", redirects, target, tenant, err)
	}
}

func TestVaultRedirectWithoutLocationIsNotSuccess(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusPermanentRedirect) })
	if _, err := c.PutVaultValues(context.Background(), "db", nil, 0); err == nil {
		t.Fatal("redirect accepted as a successful write")
	}
	if err := c.PurgeVaultEntry(context.Background(), "db", 1); err == nil {
		t.Fatal("redirect accepted as a successful purge")
	}
}
