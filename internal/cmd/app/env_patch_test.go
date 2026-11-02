package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestEnvironmentHealthChangesSurviveHTTPMergePatch(t *testing.T) {
	httpProbe := func(path string) *client.AppProbe {
		return &client.AppProbe{HTTP: &client.AppHTTPProbe{Path: path}}
	}
	tcpProbe := func() *client.AppProbe { return &client.AppProbe{TCP: &struct{}{}} }
	for _, tc := range []struct {
		name, input string
		flags       []string
		before      func(*client.AppWeb)
		after       func(*client.AppWeb)
		nulls       [][]string
	}{
		{
			name: "readiness flag tcp to http", flags: []string{"--readiness", "http:/ready"},
			after: func(w *client.AppWeb) { w.HealthChecks.Readiness = httpProbe("/ready") },
			nulls: [][]string{{"healthChecks", "readiness", "tcp"}},
		},
		{
			name: "readiness flag http to tcp", flags: []string{"--readiness", "tcp"},
			before: func(w *client.AppWeb) { w.HealthChecks.Readiness = httpProbe("/old") },
			after:  func(w *client.AppWeb) { w.HealthChecks.Readiness = tcpProbe() },
			nulls:  [][]string{{"healthChecks", "readiness", "http"}},
		},
		{
			name: "file readiness tcp to http", input: `{"web":{"healthChecks":{"readiness":{"tcp":null,"http":{"path":"/ready"}}}}}`,
			after: func(w *client.AppWeb) { w.HealthChecks.Readiness = httpProbe("/ready") },
			nulls: [][]string{{"healthChecks", "readiness", "tcp"}},
		},
		{
			name: "file readiness http to tcp", input: `{"web":{"healthChecks":{"readiness":{"http":null,"tcp":{}}}}}`,
			before: func(w *client.AppWeb) { w.HealthChecks.Readiness = httpProbe("/old") },
			after:  func(w *client.AppWeb) { w.HealthChecks.Readiness = tcpProbe() },
			nulls:  [][]string{{"healthChecks", "readiness", "http"}},
		},
		{
			name: "file liveness tcp to http", input: `{"web":{"healthChecks":{"liveness":{"tcp":null,"http":{"path":"/live"}}}}}`,
			before: func(w *client.AppWeb) { w.HealthChecks.Liveness = tcpProbe() },
			after:  func(w *client.AppWeb) { w.HealthChecks.Liveness = httpProbe("/live") },
			nulls:  [][]string{{"healthChecks", "liveness", "tcp"}},
		},
		{
			name: "file liveness http to tcp", input: `{"web":{"healthChecks":{"liveness":{"http":null,"tcp":{}}}}}`,
			after: func(w *client.AppWeb) { w.HealthChecks.Liveness = tcpProbe() },
			nulls: [][]string{{"healthChecks", "liveness", "http"}},
		},
		{
			name: "remove liveness", input: `{"web":{"healthChecks":{"liveness":null}}}`,
			after: func(w *client.AppWeb) { w.HealthChecks.Liveness = nil },
			nulls: [][]string{{"healthChecks", "liveness"}},
		},
		{
			name: "reset readiness to default", input: `{"web":{"healthChecks":{"readiness":null}}}`,
			before: func(w *client.AppWeb) { w.HealthChecks.Readiness = httpProbe("/old") },
			after:  func(w *client.AppWeb) { w.HealthChecks.Readiness = tcpProbe() },
			nulls:  [][]string{{"healthChecks", "readiness"}},
		},
		{
			name: "reset all checks", input: `{"web":{"healthChecks":null}}`,
			after: func(w *client.AppWeb) { w.HealthChecks = &client.AppHealthChecks{Readiness: tcpProbe()} },
			nulls: [][]string{{"healthChecks"}},
		},
		{
			name: "omitted checks survive unrelated edit", flags: []string{"--port", "9090"},
			after: func(w *client.AppWeb) { w.Port = 9090 },
		},
		{
			name: "nullable lists clear", input: `{"web":{"command":null,"args":null,"env":null}}`,
			after: func(w *client.AppWeb) { w.Command, w.Args, w.Env = nil, nil, nil },
			nulls: [][]string{{"command"}, {"args"}, {"env"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, expected := seededEnv(), seededEnv()
			for _, env := range []*client.AppEnvironment{current, expected} {
				env.Web.HealthChecks = &client.AppHealthChecks{Readiness: tcpProbe(), Liveness: httpProbe("/live")}
				env.Web.Command, env.Web.Args = []string{"server"}, []string{"--serve"}
				if tc.before != nil {
					tc.before(env.Web)
				}
			}
			tc.after(expected.Web)
			initial, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			var effective, patch map[string]any
			if err := json.Unmarshal(initial, &effective); err != nil {
				t.Fatal(err)
			}
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"env-uid.1"`)
				switch r.Method {
				case http.MethodGet:
					_, _ = w.Write(initial)
				case http.MethodPatch:
					writes++
					if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					applyEnvironmentWirePatch(effective, patch)
					defaultEnvironmentWireChecks(effective)
					_ = json.NewEncoder(w).Encode(effective)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			defer srv.Close()
			f := factoryWith(clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "", "test"), nil }
			args := []string{"env", "update", "shop", "production", "--wait=false", "-o", "json"}
			if tc.input != "" {
				f.IOStreams.In = strings.NewReader(tc.input)
				args = append(args, "-f", "-")
			}
			out, _, err := run(f, append(args, tc.flags...)...)
			if err != nil || writes != 1 {
				t.Fatalf("err=%v writes=%d", err, writes)
			}
			var got client.AppEnvironment
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Web, expected.Web) {
				t.Fatalf("effective web=%+v want=%+v; patch=%v", got.Web, expected.Web, patch)
			}
			for _, path := range tc.nulls {
				object := patch["web"]
				for _, key := range path {
					fields, ok := object.(map[string]any)
					if !ok {
						t.Fatalf("missing parent for null %v: %v", path, patch)
					}
					object, ok = fields[key]
					if !ok {
						t.Fatalf("missing explicit null %v: %v", path, patch)
					}
				}
				if object != nil {
					t.Fatalf("expected null %v: %v", path, patch)
				}
			}
		})
	}
}

// The HTTP fixture follows the API's recursive object merge and null removal;
// arrays and scalar values replace their previous values.
func applyEnvironmentWirePatch(current, patch map[string]any) {
	for key, next := range patch {
		if next == nil {
			delete(current, key)
		} else if fields, ok := next.(map[string]any); ok {
			prior, ok := current[key].(map[string]any)
			if !ok {
				prior = map[string]any{}
			}
			applyEnvironmentWirePatch(prior, fields)
			current[key] = prior
		} else {
			current[key] = next
		}
	}
}

// The environment CRD defaults healthChecks to {} and readiness to {tcp:{}}.
// Null removal therefore clears optional liveness but resets readiness.
func defaultEnvironmentWireChecks(environment map[string]any) {
	web := environment["web"].(map[string]any)
	checks, ok := web["healthChecks"].(map[string]any)
	if !ok {
		checks = map[string]any{}
		web["healthChecks"] = checks
	}
	if _, present := checks["readiness"]; !present {
		checks["readiness"] = map[string]any{"tcp": map[string]any{}}
	}
}

func TestEnvironmentPatchNullsDoNotChangeCreateOrResponseJSON(t *testing.T) {
	env := seededEnv()
	env.Web.HealthChecks = &client.AppHealthChecks{Readiness: &client.AppProbe{TCP: &struct{}{}}}
	for _, value := range []any{env, environmentRequest(env)} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"http":null`) || strings.Contains(string(data), `"liveness":null`) {
			t.Fatalf("patch-only nulls escaped into create/response: %s", data)
		}
	}
}

func TestEnvironmentFileProtectionNullRestoresDefaultBeforeFlagOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		flags       []string
		initial     bool
		want        bool
		patchValue  any
	}{
		{"null restores protection", `{"deletionProtection":null}`, nil, false, true, true},
		{"absent retains current false", `{}`, nil, false, false, nil},
		{"absent retains current true", `{}`, nil, true, true, nil},
		{"explicit false", `{"deletionProtection":false}`, nil, true, false, false},
		{"flag overrides null", `{"deletionProtection":null}`, []string{"--deletion-protection=false"}, false, false, nil},
		{"negative flag overrides null", `{"deletionProtection":null}`, []string{"--no-deletion-protection"}, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := seededEnv()
			current.DeletionProtection = tc.initial
			initial, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			var effective, patch map[string]any
			if err := json.Unmarshal(initial, &effective); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"env-uid.1"`)
				if r.Method == http.MethodGet {
					_, _ = w.Write(initial)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				applyEnvironmentWirePatch(effective, patch)
				_ = json.NewEncoder(w).Encode(effective)
			}))
			defer srv.Close()
			f := factoryWith(clienttest.New())
			f.Client = func() (client.Interface, error) { return client.New(srv.URL, "acme", "", "test"), nil }
			f.IOStreams.In = strings.NewReader(tc.input)
			args := []string{"env", "update", "shop", "production", "-f", "-", "--port", "9090", "--wait=false", "-o", "json"}
			out, _, err := run(f, append(args, tc.flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			var got client.AppEnvironment
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			if got.DeletionProtection != tc.want || patch["deletionProtection"] != tc.patchValue {
				t.Fatalf("effective protection=%v patch=%v", got.DeletionProtection, patch)
			}
			_, present := patch["deletionProtection"]
			if present != (tc.patchValue != nil) {
				t.Fatalf("protection presence=%v patch=%v", present, patch)
			}
		})
	}
}
