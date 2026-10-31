package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

const digest = "ghcr.io/kupecloud/hello-kupe@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestEnvironmentResourceFlags(t *testing.T) {
	for _, tc := range []struct {
		name             string
		flags            []string
		code             int
		cpu, limit, hint string
	}{
		{"defaults", nil, 0, "250m", "1", ""},
		{"request", []string{"--cpu", "500m"}, 0, "500m", "1", ""},
		{"over-limit", []string{"--cpu", "2"}, 2, "", "", "--cpu-limit"},
		{"raised-limit", []string{"--cpu", "2", "--cpu-limit", "2"}, 0, "2", "2", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(fake)
			args := append([]string{"env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080", "--wait=false"}, tc.flags...)
			_, _, err := run(f, args...)
			if cli.ExitCode(err) != tc.code {
				t.Fatalf("err=%v code=%d", err, cli.ExitCode(err))
			}
			if tc.code != 0 {
				if len(fake.Calls) != 0 || !strings.Contains(err.Error(), tc.hint) {
					t.Fatalf("calls=%v err=%v", fake.Calls, err)
				}
				return
			}
			r := fake.AppEnvironmentCreates[0].Web.Resources
			if r.Requests.CPU != tc.cpu || r.Limits.CPU != tc.limit || r.Requests.Memory != "256Mi" || r.Limits.Memory != "512Mi" {
				t.Fatalf("resources=%+v", r)
			}
		})
	}
}
func seededEnv() *client.AppEnvironment {
	n := int32(1)
	return &client.AppEnvironment{Name: "shop-production", App: "shop", Environment: "production", Cluster: "europe", Generation: 1, Web: &client.AppWeb{Image: digest, Port: 8080, Replicas: &n, Resources: &client.AppResources{Requests: client.AppQuantities{CPU: "500m", Memory: "256Mi"}, Limits: client.AppQuantities{CPU: "2", Memory: "512Mi"}}, Env: []client.AppEnvVar{{Name: "A", Value: str("old")}, {Name: "B", Value: str("kept")}}}, Status: &client.AppEnvironmentStatus{Phase: client.PhaseRunning, ObservedGeneration: 1}}
}
func str(s string) *string { return &s }
func TestEnvironmentUpdateKeepsListsAndResources(t *testing.T) {
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": seededEnv()}
	_, _, err := run(factoryWith(fake), "env", "update", "shop", "production", "--env", "A=new", "--cpu-limit", "4", "--wait=false")
	if err != nil {
		t.Fatal(err)
	}
	p := fake.AppEnvironmentPatches[0]
	if len(p.Web.Env) != 2 || *p.Web.Env[1].Value != "kept" || p.Web.Resources.Requests.CPU != "500m" || p.Web.Resources.Limits.CPU != "4" {
		t.Fatalf("patch=%+v", p.Web)
	}
	_, _, err = run(factoryWith(fake), "env", "update", "shop", "production", "--unset-env", "B", "--wait=false")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.AppEnvironmentPatches[1].Web.Env) != 1 {
		t.Fatal("unset env dropped/retained wrong entries")
	}
	_, _, err = run(factoryWith(fake), "env", "update", "shop", "production", "--memory", "1Gi", "--wait=false")
	if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), "--memory-limit") {
		t.Fatalf("err=%v", err)
	}
}

func TestEnvironmentFilesAndLocalRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		flags       []string
		code        int
		contains    string
	}{
		{"yaml", "cluster: europe\nweb:\n  image: " + digest + "\n  port: 8080\n", []string{"--port", "9090"}, 0, ""},
		{"json", `{"cluster":"europe","web":{"image":"` + digest + `","port":8080}}`, nil, 0, ""},
		{"unknown", "cluster: europe\nweb:\n  surprise: yes\n", nil, 2, "surprise"},
		{"duplicate", "cluster: europe\ncluster: other\n", nil, 2, "duplicate"},
		{"multi", "cluster: europe\n---\ncluster: other\n", nil, 2, "one document"},
		{"type", `{"cluster":"europe","web":{"image":"` + digest + `","port":"8080"}}`, nil, 2, "type"},
		{"alias", "cluster: &cluster europe\nweb: *cluster\n", nil, 2, "aliases"},
		{"tag", "cluster: europe\nweb:\n  image: nginx:latest\n  port: 8080\n", nil, 2, "digest"},
		{"override-tag", "cluster: europe\nweb:\n  image: nginx:latest\n  port: 8080\n", []string{"--image", digest}, 0, ""},
		{"oversize", strings.Repeat("x", maxEnvironmentFile+1), nil, 2, "1 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, stdin := range []bool{false, true} {
				fake := clienttest.New()
				f := factoryWith(fake)
				file := "-"
				if stdin {
					f.IOStreams.In = strings.NewReader(tc.input)
				} else {
					file = filepath.Join(t.TempDir(), "env.yaml")
					if err := os.WriteFile(file, []byte(tc.input), 0600); err != nil {
						t.Fatal(err)
					}
				}
				args := append([]string{"env", "create", "shop", "production", "-f", file, "--wait=false"}, tc.flags...)
				_, _, err := run(f, args...)
				if cli.ExitCode(err) != tc.code {
					t.Fatalf("stdin=%v code=%d err=%v", stdin, cli.ExitCode(err), err)
				}
				if err != nil {
					if len(fake.Calls) > 0 || !strings.Contains(err.Error(), tc.contains) {
						t.Fatalf("calls=%v err=%v", fake.Calls, err)
					}
				} else if tc.name == "yaml" && fake.AppEnvironmentCreates[0].Web.Port != 9090 {
					t.Fatal("flag did not override file")
				}
			}
		})
	}
}

func TestEnvironmentUpdateRefusesIdentityBeforeRequest(t *testing.T) {
	for _, field := range []string{"environment", "cluster", "app", "deploymentRevision"} {
		t.Run(field, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(fake)
			f.IOStreams.In = strings.NewReader(`{"` + field + `":"changed"}`)
			_, _, err := run(f, "env", "update", "shop", "production", "-f", "-")
			if cli.ExitCode(err) != 2 || !strings.Contains(err.Error(), field) || len(fake.Calls) > 0 {
				t.Fatalf("err=%v calls=%v", err, fake.Calls)
			}
		})
	}
	fake := clienttest.New()
	_, _, err := run(factoryWith(fake), "env", "update", "shop", "production", "--cluster", "other")
	if cli.ExitCode(err) != 2 || len(fake.Calls) > 0 {
		t.Fatalf("err=%v calls=%v", err, fake.Calls)
	}
	fake = clienttest.New()
	f := factoryWith(fake)
	f.IOStreams.In = strings.NewReader(`{"web":{"image":"nginx:latest"}}`)
	_, _, err = run(f, "env", "update", "shop", "production", "-f", "-")
	if cli.ExitCode(err) != 2 || len(fake.Calls) > 0 {
		t.Fatalf("file tag err=%v calls=%v", err, fake.Calls)
	}
}

func TestEnvironmentRMWRetainsUnrelatedChangesAndRefusesTargetConflict(t *testing.T) {
	for _, targetChanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "untouched", true: "target"}[targetChanged], func(t *testing.T) {
			fake := clienttest.New()
			first, next := seededEnv(), seededEnv()
			next.Generation = 2
			if targetChanged {
				next.Web.Env[0].Value = str("other writer")
			} else {
				next.Web.Env[1].Value = str("concurrent B")
			}
			fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": next}
			fake.GetAppEnvironmentSeq["shop"] = map[string][]*client.AppEnvironment{"production": {first, next}}
			fake.UpdateAppEnvironmentErrors = []error{&client.APIError{StatusCode: 412, Code: "precondition_failed"}, nil}
			_, _, err := run(factoryWith(fake), "env", "update", "shop", "production", "--env", "A=new", "--wait=false")
			if targetChanged {
				if cli.ExitCode(err) != 5 || len(fake.AppEnvironmentPatches) > 0 {
					t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(fake.AppEnvironmentPatches) != 1 || *fake.AppEnvironmentPatches[0].Web.Env[1].Value != "concurrent B" {
				t.Fatalf("patches=%+v", fake.AppEnvironmentPatches)
			}
		})
	}
}

func TestEnvironmentClearListsReachesJSON(t *testing.T) {
	fake := clienttest.New()
	env := seededEnv()
	env.Web.Env = env.Web.Env[:1]
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": env}
	_, _, err := run(factoryWith(fake), "env", "update", "shop", "production", "--unset-env", "A", "--wait=false")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(fake.AppEnvironmentPatches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"env":[]`) {
		t.Fatalf("cleared env omitted: %s", data)
	}
}

func TestEnvironmentWaitUsesNewGenerationAndReportsRequestedRelease(t *testing.T) {
	fake := clienttest.New()
	first, stale, settled := seededEnv(), seededEnv(), seededEnv()
	settled.Generation = 2
	settled.Status.ObservedGeneration = 2
	settled.Status.RequestedRelease = &client.AppReleaseReference{Name: "shop-production-2"}
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": first}
	fake.GetAppEnvironmentSeq["shop"] = map[string][]*client.AppEnvironment{"production": {first, stale, settled}}
	_, stderr, err := run(factoryWith(fake), "env", "update", "shop", "production", "--env", "A=new")
	if err != nil {
		t.Fatal(err)
	}
	gets := 0
	for _, call := range fake.Calls {
		if call == "GetAppEnvironment:production" {
			gets++
		}
	}
	if gets != 3 || !strings.Contains(stderr, "deploying release 2") {
		t.Fatalf("calls=%v stderr=%s", fake.Calls, stderr)
	}
}

func TestEnvironmentWaitTimeoutNamesNestedStatusCommand(t *testing.T) {
	fake := clienttest.New()
	current, stale := seededEnv(), seededEnv()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": current}
	fake.GetAppEnvironmentSeq["shop"] = map[string][]*client.AppEnvironment{"production": {current, stale}}
	_, _, err := run(factoryWith(fake), "env", "update", "shop", "production", "--env", "A=new", "--wait-timeout", "1ms")
	var ce *cli.Error
	if cli.ExitCode(err) != 8 || !errors.As(err, &ce) || !strings.Contains(ce.Hint, "kupe app env get shop production") {
		t.Fatalf("err=%v hint=%v", err, ce)
	}
}

func TestEnvironmentEffectiveResourcesOnHTTPWire(t *testing.T) {
	for _, tc := range []struct {
		name               string
		update, conflict   bool
		flags              []string
		want               [4]string
		code, gets, writes int
	}{
		{"create-request", false, false, []string{"--cpu", "500m"}, [4]string{"500m", "256Mi", "1", "512Mi"}, 0, 0, 1},
		{"create-request-and-limit", false, false, []string{"--cpu", "2", "--cpu-limit", "2"}, [4]string{"2", "256Mi", "2", "512Mi"}, 0, 0, 1},
		{"update-limit-only", true, false, []string{"--cpu-limit", "4"}, [4]string{"500m", "256Mi", "4", "512Mi"}, 0, 1, 1},
		{"update-memory-over-limit", true, false, []string{"--memory", "1Gi"}, [4]string{}, 2, 1, 0},
		{"concurrent-limit-revalidated", true, true, []string{"--cpu", "1"}, [4]string{"1", "256Mi", "2", "512Mi"}, 2, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gets, writes := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					gets++
					env := seededEnv()
					if tc.conflict && gets == 2 {
						env.Generation = 2
						env.Web.Resources.Limits.CPU = "500m"
					}
					w.Header().Set("ETag", fmt.Sprintf(`"uid.%d"`, gets))
					_ = json.NewEncoder(w).Encode(env)
					return
				}
				writes++
				var body struct {
					Web struct {
						Resources client.AppResources `json:"resources"`
					} `json:"web"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				resources := body.Web.Resources
				got := [4]string{resources.Requests.CPU, resources.Requests.Memory, resources.Limits.CPU, resources.Limits.Memory}
				if got != tc.want {
					t.Errorf("wire effective values=%v want=%v", got, tc.want)
				}
				if tc.conflict {
					w.WriteHeader(412)
					_, _ = w.Write([]byte(`{"code":"precondition_failed","error":"changed"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(seededEnv())
			}))
			defer srv.Close()
			f := factoryWith(clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "", "test"), nil }
			verb := "create"
			if tc.update {
				verb = "update"
			}
			args := []string{"env", verb, "shop", "production", "--wait=false"}
			if !tc.update {
				args = append(args, "--cluster", "europe", "--image", digest, "--port", "8080")
			}
			args = append(args, tc.flags...)
			_, _, err := run(f, args...)
			if cli.ExitCode(err) != tc.code || gets != tc.gets || writes != tc.writes {
				t.Fatalf("exit=%d err=%v gets=%d writes=%d", cli.ExitCode(err), err, gets, writes)
			}
		})
	}
}

func TestEnvironmentFileListPresenceSemantics(t *testing.T) {
	for _, tc := range []struct{ name, input, field, want string }{
		{"empty-env", `{"web":{"env":[]}}`, "env", "[]"},
		{"null-env", `{"web":{"env":null}}`, "env", "null"},
		{"empty-domains", `{"domains":[]}`, "domains", "[]"},
		{"null-domains", `{"domains":null}`, "domains", "null"},
		{"empty-secrets", `{"secrets":[]}`, "secrets", "[]"},
		{"null-secrets", `{"secrets":null}`, "secrets", "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := clienttest.New()
			env := seededEnv()
			env.Secrets = []client.AppSecret{{Name: "payments", Path: "apps/shop/payments"}}
			env.Domains = []string{"shop.example.com"}
			fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": env}
			f := factoryWith(fake)
			f.IOStreams.In = strings.NewReader(tc.input)
			_, _, err := run(f, "env", "update", "shop", "production", "-f", "-", "--wait=false")
			if err != nil {
				t.Fatal(err)
			}
			if len(fake.AppEnvironmentPatches) != 1 {
				t.Fatalf("patches=%v", fake.AppEnvironmentPatches)
			}
			data, err := json.Marshal(fake.AppEnvironmentPatches[0])
			if err != nil {
				t.Fatal(err)
			}
			var patch map[string]json.RawMessage
			if err := json.Unmarshal(data, &patch); err != nil {
				t.Fatal(err)
			}
			field := patch[tc.field]
			if tc.field == "env" {
				var web map[string]json.RawMessage
				if err := json.Unmarshal(patch["web"], &web); err != nil {
					t.Fatal(err)
				}
				field = web["env"]
			}
			if string(field) != tc.want {
				t.Fatalf("patch=%s want %s=%s", data, tc.field, tc.want)
			}
			if tc.field != "domains" && patch["domains"] != nil {
				t.Fatal("omitted domains were replaced")
			}
			if tc.field != "secrets" && patch["secrets"] != nil {
				t.Fatal("omitted secrets were replaced")
			}
		})
	}
}

func TestEnvironmentFileLocalPathForms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.yaml")
	input := "cluster: europe\nweb:\n  image: " + digest + "\n  port: 8080\n"
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(wd, path)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, relative, link} {
		fake := clienttest.New()
		_, _, err := run(factoryWith(fake), "env", "create", "shop", "production", "-f", file, "--wait=false")
		if err != nil || len(fake.AppEnvironmentCreates) != 1 {
			t.Fatalf("path=%s err=%v", file, err)
		}
	}
}
