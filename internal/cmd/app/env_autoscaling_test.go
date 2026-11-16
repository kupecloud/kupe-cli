package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func envWithAutoscaling(a *client.AppAutoscaling) *client.AppEnvironment {
	e := seededEnv()
	e.Web.Autoscaling = a
	return e
}

func updateEnv(t *testing.T, env *client.AppEnvironment, args ...string) (*clienttest.Fake, error) {
	t.Helper()
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": env}
	_, _, err := run(factoryWith(fake), append([]string{"env", "update", "shop", "production", "--wait=false"}, args...)...)
	return fake, err
}

func TestEnvironmentAutoscalingFlags(t *testing.T) {
	on := func() *client.AppAutoscaling { return &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70} }
	for _, tc := range []struct {
		name   string
		before *client.AppAutoscaling
		args   []string
		want   *client.AppAutoscaling
	}{
		{"turn on with all three", nil, []string{"--min-pods", "2", "--max-pods", "6", "--cpu-percent", "60"}, &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 60}},
		{"turn on without cpu-percent leaves it unset", nil, []string{"--min-pods", "1", "--max-pods", "3"}, &client.AppAutoscaling{MinPods: 1, MaxPods: 3}},
		{"change only max", on(), []string{"--max-pods", "8"}, &client.AppAutoscaling{MinPods: 2, MaxPods: 8, CPUPercent: 70}},
		{"change only min", on(), []string{"--min-pods", "3"}, &client.AppAutoscaling{MinPods: 3, MaxPods: 6, CPUPercent: 70}},
		{"change only cpu", on(), []string{"--cpu-percent", "50"}, &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 50}},
		{"turn off", on(), []string{"--no-autoscaling"}, nil},
		{"turn off with replicas", on(), []string{"--no-autoscaling", "--replicas", "3"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := updateEnv(t, envWithAutoscaling(tc.before), tc.args...)
			if err != nil || len(fake.AppEnvironmentPatches) != 1 {
				t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
			}
			got := fake.AppEnvironmentPatches[0].Web.Autoscaling
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("autoscaling=%+v want=%+v", got, tc.want)
			}
		})
	}
}

// The wire body carries "autoscaling": null only when it is turned off, the
// whole object when it changes, and no key when the environment has none.
func TestEnvironmentAutoscalingOnTheWire(t *testing.T) {
	body := func(env *client.AppEnvironment, args ...string) map[string]any {
		fake, err := updateEnv(t, env, args...)
		if err != nil || len(fake.AppEnvironmentPatches) != 1 {
			t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
		}
		data, err := json.Marshal(fake.AppEnvironmentPatches[0])
		if err != nil {
			t.Fatal(err)
		}
		var patch map[string]any
		if err := json.Unmarshal(data, &patch); err != nil {
			t.Fatal(err)
		}
		return patch["web"].(map[string]any)
	}
	on := &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70}
	if web := body(envWithAutoscaling(on), "--no-autoscaling"); web["autoscaling"] != nil || !has(web, "autoscaling") {
		t.Fatalf("turn off must send an explicit null: %v", web)
	}
	if web := body(envWithAutoscaling(on), "--max-pods", "8"); web["autoscaling"].(map[string]any)["maxPods"] != float64(8) || web["autoscaling"].(map[string]any)["minPods"] != float64(2) {
		t.Fatalf("partial change must keep the other values: %v", web)
	}
	// An environment with no autoscaling still sends null for it: the merge patch removes nothing.
	if web := body(envWithAutoscaling(nil), "--port", "9090"); web["autoscaling"] != nil {
		t.Fatalf("unexpected autoscaling: %v", web)
	}
	// A file's null turns autoscaling off.
	fake := clienttest.New()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": envWithAutoscaling(on)}
	f := factoryWith(fake)
	f.IOStreams.In = strings.NewReader(`{"web":{"autoscaling":null}}`)
	if _, _, err := run(f, "env", "update", "shop", "production", "--wait=false", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	if fake.AppEnvironmentPatches[0].Web.Autoscaling != nil {
		t.Fatalf("file null kept autoscaling: %+v", fake.AppEnvironmentPatches[0].Web.Autoscaling)
	}
}

func has(m map[string]any, k string) bool { _, ok := m[k]; return ok }

func TestEnvironmentCreateWithoutAutoscalingHasNoKey(t *testing.T) {
	fake := flowFake()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{}
	if _, _, err := run(factoryWith(fake), "env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080", "--wait=false"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(fake.AppEnvironmentCreates[0])
	if strings.Contains(string(data), "autoscaling") {
		t.Fatalf("create body: %s", data)
	}
	fake = flowFake()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{}
	if _, _, err := run(factoryWith(fake), "env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080", "--min-pods", "2", "--max-pods", "4", "--wait=false"); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(fake.AppEnvironmentCreates[0].Web.Autoscaling)
	if string(data) != `{"minPods":2,"maxPods":4}` {
		t.Fatalf("create autoscaling: %s", data)
	}
}

func TestEnvironmentAutoscalingMisuse(t *testing.T) {
	on := &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70}
	const replicasMsg = "autoscaling is on for this environment, so --replicas has no effect; add --no-autoscaling to run a fixed number of pods, or change --min-pods and --max-pods"
	for _, tc := range []struct {
		name   string
		before *client.AppAutoscaling
		args   []string
		want   string
	}{
		{"on needs max", nil, []string{"--min-pods", "2"}, "to turn on autoscaling, pass --min-pods and --max-pods together"},
		{"on needs min", nil, []string{"--max-pods", "2"}, "to turn on autoscaling, pass --min-pods and --max-pods together"},
		{"on with only cpu", nil, []string{"--cpu-percent", "50"}, "to turn on autoscaling, pass --min-pods and --max-pods together"},
		{"off with a pods flag", on, []string{"--no-autoscaling", "--min-pods", "2"}, "--no-autoscaling cannot be combined with --min-pods, --max-pods or --cpu-percent"},
		{"off with cpu", on, []string{"--no-autoscaling", "--cpu-percent", "50"}, "--no-autoscaling cannot be combined with --min-pods, --max-pods or --cpu-percent"},
		{"replicas while on", on, []string{"--replicas", "3"}, replicasMsg},
		{"replicas with a pods change", on, []string{"--replicas", "3", "--max-pods", "8"}, replicasMsg},
		{"min out of range", nil, []string{"--min-pods", "0", "--max-pods", "4"}, "min-pods and max-pods must be between 1 and 10"},
		{"max out of range", nil, []string{"--min-pods", "1", "--max-pods", "11"}, "min-pods and max-pods must be between 1 and 10"},
		{"range beats min over max", nil, []string{"--min-pods", "11", "--max-pods", "3"}, "min-pods and max-pods must be between 1 and 10"},
		{"range beats cpu", nil, []string{"--min-pods", "0", "--max-pods", "3", "--cpu-percent", "5"}, "min-pods and max-pods must be between 1 and 10"},
		{"min over max", nil, []string{"--min-pods", "5", "--max-pods", "3"}, "min-pods cannot be more than max-pods"},
		{"min over max beats cpu", nil, []string{"--min-pods", "5", "--max-pods", "3", "--cpu-percent", "5"}, "min-pods cannot be more than max-pods"},
		{"cpu too low", nil, []string{"--min-pods", "1", "--max-pods", "3", "--cpu-percent", "9"}, "cpu-percent must be between 10 and 100"},
		{"cpu too high", on, []string{"--cpu-percent", "101"}, "cpu-percent must be between 10 and 100"},
		{"cpu zero", on, []string{"--cpu-percent", "0"}, "cpu-percent must be between 10 and 100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := updateEnv(t, envWithAutoscaling(tc.before), tc.args...)
			if cli.ExitCode(err) != 2 || err.Error() != tc.want || len(fake.AppEnvironmentPatches) != 0 {
				t.Fatalf("exit=%d err=%v patches=%v", cli.ExitCode(err), err, fake.AppEnvironmentPatches)
			}
		})
	}
}

func TestDeployRefusesReplicasWhileAutoscaling(t *testing.T) {
	fake := flowFake()
	fake.AppsByName["shop"] = &client.App{Name: "shop"}
	e := releaseEnvironment()
	e.Web.Autoscaling = &client.AppAutoscaling{MinPods: 2, MaxPods: 6}
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": e}
	_, _, err := run(factoryWith(fake), "deploy", "shop", "production", "--image", nextDigest, "--replicas", "3", "--yes")
	var ce *cli.Error
	if cli.ExitCode(err) != 2 || !errors.As(err, &ce) || !strings.HasPrefix(err.Error(), "autoscaling is on for this environment, so --replicas has no effect") || ce.Hint != "kupe app env update APP ENV --no-autoscaling --replicas N" || len(writes(fake)) != 0 {
		t.Fatalf("exit=%d err=%v writes=%v", cli.ExitCode(err), err, writes(fake))
	}
	// Without --replicas the deploy goes through and keeps autoscaling.
	if _, _, err := run(factoryWith(fake), "deploy", "shop", "production", "--image", nextDigest, "--yes", "--wait=false"); err != nil {
		t.Fatal(err)
	}
	if got := fake.AppEnvironmentPatches[0].Web.Autoscaling; got == nil || got.MinPods != 2 || got.MaxPods != 6 {
		t.Fatalf("autoscaling=%+v", got)
	}
}

// An unrelated edit sends the whole web snapshot, so it must carry the current
// autoscaling unchanged; dropping it would make the merge patch keep it only by
// luck. Flipping deletion protection touches no web field and sends no web key.
func TestEnvironmentUnrelatedEditsKeepAutoscalingOnTheWire(t *testing.T) {
	wantAutoscaling := map[string]any{"minPods": float64(2), "maxPods": float64(6), "cpuPercent": float64(70)}
	for _, tc := range []struct {
		name, input string
		flags       []string
		noWeb       bool
	}{
		{name: "port", flags: []string{"--port", "9090"}},
		{name: "set-env", flags: []string{"--set-env", "NEW=1"}},
		{name: "image", flags: []string{"--image", nextDigest}},
		{name: "readiness", flags: []string{"--readiness", "tcp"}},
		{name: "file without autoscaling", input: `{"web":{"port":9090}}`},
		{name: "deletion protection", flags: []string{"--deletion-protection=true"}, noWeb: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := envWithAutoscaling(&client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70})
			initial, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			var patch map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"env-uid.1"`)
				switch r.Method {
				case http.MethodGet:
					_, _ = w.Write(initial)
				case http.MethodPatch:
					if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
						t.Error(err)
					}
					_, _ = w.Write(initial)
				default:
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
			if _, _, err := run(f, append(args, tc.flags...)...); err != nil {
				t.Fatal(err)
			}
			if patch == nil {
				t.Fatal("no PATCH was sent")
			}
			web, present := patch["web"]
			if tc.noWeb {
				if present {
					t.Fatalf("web must not be sent: %v", patch)
				}
				return
			}
			got, _ := web.(map[string]any)["autoscaling"].(map[string]any)
			if !reflect.DeepEqual(got, wantAutoscaling) {
				t.Fatalf("autoscaling=%v patch=%v", got, patch)
			}
		})
	}
}

func TestDeployKeepsAutoscalingInTheRequest(t *testing.T) {
	fake := flowFake()
	fake.AppsByName["shop"] = &client.App{Name: "shop"}
	e := releaseEnvironment()
	e.Web.Autoscaling = &client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70}
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": e}
	if _, _, err := run(factoryWith(fake), "deploy", "shop", "production", "--image", nextDigest, "--yes", "--wait=false"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(fake.AppEnvironmentPatches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"autoscaling":{"minPods":2,"maxPods":6,"cpuPercent":70}`) {
		t.Fatalf("patch=%s", data)
	}
}

func TestEnvironmentRMWRefusesConcurrentChangeToFlaggedAutoscaling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		changed bool
	}{
		{"flagged max-pods changed", []string{"--max-pods", "8"}, true},
		{"unflagged max-pods changed", []string{"--min-pods", "3"}, false},
		{"unflagged cpu-percent changed", []string{"--max-pods", "8"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := envWithAutoscaling(&client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70})
			next := envWithAutoscaling(&client.AppAutoscaling{MinPods: 2, MaxPods: 6, CPUPercent: 70})
			next.Generation = 2
			switch tc.name {
			case "flagged max-pods changed":
				next.Web.Autoscaling.MaxPods = 9
			case "unflagged max-pods changed":
				next.Web.Autoscaling.MaxPods = 9
			default:
				next.Web.Autoscaling.CPUPercent = 50
			}
			fake := clienttest.New()
			fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": next}
			fake.GetAppEnvironmentSeq["shop"] = map[string][]*client.AppEnvironment{"production": {first, next}}
			fake.UpdateAppEnvironmentErrors = []error{&client.APIError{StatusCode: 412, Code: "precondition_failed"}, nil}
			_, _, err := run(factoryWith(fake), append([]string{"env", "update", "shop", "production", "--wait=false"}, tc.args...)...)
			if tc.changed {
				if cli.ExitCode(err) != 5 || len(fake.AppEnvironmentPatches) != 0 {
					t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
				}
				return
			}
			if err != nil || len(fake.AppEnvironmentPatches) != 1 {
				t.Fatalf("err=%v patches=%v", err, fake.AppEnvironmentPatches)
			}
		})
	}
}

func TestEnvironmentCreateAllowsReplicasWithAutoscaling(t *testing.T) {
	fake := flowFake()
	fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{}
	if _, _, err := run(factoryWith(fake), "env", "create", "shop", "production", "--cluster", "europe", "--image", digest, "--port", "8080", "--replicas", "3", "--min-pods", "2", "--max-pods", "4", "--wait=false"); err != nil {
		t.Fatal(err)
	}
	w := fake.AppEnvironmentCreates[0].Web
	if *w.Replicas != 3 || w.Autoscaling == nil || w.Autoscaling.MaxPods != 4 {
		t.Fatalf("web=%+v", w)
	}
}

func TestEnvironmentFileMisuse(t *testing.T) {
	for _, tc := range []struct{ name, verb, input, want string }{
		{"replicas checked before autoscaling", "create", `{"web":{"replicas":20,"autoscaling":{"minPods":0,"maxPods":3}}}`, "replicas must be between 1 and 10"},
		{"explicit zero cpuPercent", "update", `{"web":{"autoscaling":{"minPods":1,"maxPods":3,"cpuPercent":0}}}`, "cpu-percent must be between 10 and 100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := flowFake()
			fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": seededEnv()}
			args := []string{"env", tc.verb, "shop", "production", "--wait=false", "-f", "-"}
			if tc.verb == "create" {
				delete(fake.AppEnvironmentsByApp["shop"], "production")
				args = append(args, "--cluster", "europe", "--image", digest, "--port", "8080")
			}
			f := factoryWith(fake)
			f.IOStreams.In = strings.NewReader(tc.input)
			_, _, err := run(f, args...)
			if cli.ExitCode(err) != 2 || err.Error() != tc.want || len(writes(fake)) != 0 {
				t.Fatalf("exit=%d err=%v writes=%v", cli.ExitCode(err), err, writes(fake))
			}
		})
	}
}
