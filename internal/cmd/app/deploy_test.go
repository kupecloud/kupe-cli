package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

const nextDigest = "ghcr.io/kupecloud/hello-kupe@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func releaseEnvironment() *client.AppEnvironment {
	e := seededEnv()
	e.UID, e.DeploymentRevision = "current-environment-uid", 1
	e.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-1"}
	return e
}
func releaseFake() *clienttest.Fake {
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": releaseEnvironment()}
	fake.GetAppEnvironmentSeq["shop"] = map[string][]*client.AppEnvironment{}
	return fake
}

func TestDeployTagUsesOneResolutionAndPreservesEffectiveConfigurationOnWire(t *testing.T) {
	current := releaseEnvironment()
	current.Web.Command, current.Web.Args = []string{"/app/server"}, []string{"--serve", "web"}
	current.Web.HealthChecks = &client.AppHealthChecks{Readiness: &client.AppProbe{HTTP: &client.AppHTTPProbe{Path: "/ready"}}, Liveness: &client.AppProbe{TCP: &struct{}{}}}
	current.Web.Env = append(current.Web.Env, client.AppEnvVar{Name: "DB_PASSWORD", ValueFrom: &client.AppValueFrom{SecretKeyRef: client.AppSecretKeyRef{Secret: "database", Key: "PASSWORD"}}})
	current.Secrets = []client.AppSecret{{Name: "database", Path: "apps/shop/database"}}
	current.Domains, current.DeletionProtection = []string{"shop.example.com"}, true
	const privateValue = "synthetic-private-literal"
	current.Web.Env[0].Value = str(privateValue)
	resolve, patches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/tenants/acme/app-images/resolve" {
			resolve++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["image"] != "hello-kupe:release" {
				t.Errorf("resolve body=%v err=%v", body, err)
			}
			fmt.Fprintf(w, `{"reference":%q}`, nextDigest)
			return
		}
		if r.URL.Path != "/api/v1/tenants/acme/apps/shop/environments/production" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("ETag", `"current-environment-uid.1"`)
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(current)
			return
		}
		patches++
		if r.Method != "PATCH" || r.Header.Get("If-Match") != `"current-environment-uid.1"` {
			t.Errorf("method=%s etag=%q", r.Method, r.Header.Get("If-Match"))
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body) != 1 || body["web"] == nil {
			t.Errorf("unrelated top-level fields=%v", body)
		}
		var web client.AppWeb
		if err := json.Unmarshal(body["web"], &web); err != nil {
			t.Error(err)
		}
		want := *current.Web
		want.Image = nextDigest
		if !reflect.DeepEqual(web, want) {
			t.Errorf("deploy changed effective configuration: got=%+v want=%+v", web, want)
		}
		accepted := *current
		accepted.Generation, accepted.Web = 2, &web
		_ = json.NewEncoder(w).Encode(accepted)
	}))
	defer server.Close()
	f := factoryWith(clienttest.New())
	f.Client = func() (client.Interface, error) { return client.New(server.URL, "acme", "", "test"), nil }
	out, review, err := run(f, "deploy", "shop", "production", "--image", "hello-kupe:release", "--yes", "--wait=false", "-o", "json")
	if err != nil || resolve != 1 || patches != 1 {
		t.Fatalf("resolve=%d patches=%d err=%v", resolve, patches, err)
	}
	if !strings.Contains(review, "hello-kupe:release → "+nextDigest) || strings.Contains(review, privateValue) || !strings.Contains(review, "DB_PASSWORD (secret reference)") {
		t.Fatalf("review=%s", review)
	}
	var result client.AppEnvironment
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Web.Image != nextDigest || result.Cluster != current.Cluster || !result.DeletionProtection {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestDeployFlagsMergeVariablesPortAndReplicasWithoutLosingSecretReferences(t *testing.T) {
	fake := releaseFake()
	before := fake.AppEnvironmentsByApp["shop"]["production"]
	before.Web.Env = append(before.Web.Env, client.AppEnvVar{Name: "SECRET", ValueFrom: &client.AppValueFrom{SecretKeyRef: client.AppSecretKeyRef{Secret: "s", Key: "KEY"}}})
	before.Secrets = []client.AppSecret{{Name: "s", Path: "apps/s"}}
	_, _, err := run(factoryWith(fake), "deploy", "shop", "production", "--image", nextDigest, "--set-env", "A=updated", "--set-env", "NEW=added", "--port", "9090", "--replicas", "3", "--yes", "--wait=false")
	if err != nil || len(fake.ResolvedImages) != 0 || len(fake.AppEnvironmentPatches) != 1 {
		t.Fatalf("err=%v calls=%v", err, fake.Calls)
	}
	p := fake.AppEnvironmentPatches[0]
	if p.Secrets != nil || p.Domains != nil || p.DeletionProtection != nil || p.Web.Port != 9090 || *p.Web.Replicas != 3 || len(p.Web.Env) != 4 || *p.Web.Env[0].Value != "updated" || *p.Web.Env[1].Value != "kept" || p.Web.Env[2].ValueFrom.SecretKeyRef.Secret != "s" || p.Web.Resources.Limits.CPU != "2" {
		t.Fatalf("patch=%+v", p)
	}
}

func TestDeployAndRetryNeedConfirmationBeforeAnyWrite(t *testing.T) {
	for _, args := range [][]string{{"deploy", "shop", "production", "--image", nextDigest, "--wait=false"}, {"retry", "shop", "production", "--wait=false"}} {
		fake := releaseFake()
		_, _, err := run(factoryWith(fake), args...)
		if cli.ExitCode(err) != 2 || len(fake.AppEnvironmentPatches) != 0 || len(fake.AppRetryETags) != 0 {
			t.Fatalf("args=%v err=%v calls=%v", args, err, fake.Calls)
		}
	}
}

func TestReleaseWritesReReview412WithoutResubmissionEvenWithYes(t *testing.T) {
	for _, verb := range []string{"deploy", "retry"} {
		t.Run(verb, func(t *testing.T) {
			gets, writes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					gets++
					current := releaseEnvironment()
					current.Generation = int64(gets)
					if gets > 1 {
						current.Web.Port = 9090
					}
					w.Header().Set("ETag", fmt.Sprintf(`"current-environment-uid.%d"`, gets))
					_ = json.NewEncoder(w).Encode(current)
					return
				}
				writes++
				if r.Header.Get("If-Match") != `"current-environment-uid.1"` {
					t.Errorf("review etag=%s", r.Header.Get("If-Match"))
				}
				w.WriteHeader(412)
				fmt.Fprint(w, `{"code":"precondition_failed","message":"stale"}`)
			}))
			defer server.Close()
			f := factoryWith(clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(server.URL, "acme", "", "test"), nil }
			args := []string{verb, "shop", "production", "--yes", "--wait=false"}
			if verb == "deploy" {
				args = append(args, "--image", nextDigest)
			}
			_, review, err := run(f, args...)
			if cli.ExitCode(err) != 5 || gets != 2 || writes != 1 || !strings.Contains(review, "generation 2") || !strings.Contains(review, "port: 9090") {
				t.Fatalf("gets=%d writes=%d review=%s err=%v", gets, writes, review, err)
			}
		})
	}
}
