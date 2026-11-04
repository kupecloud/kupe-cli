package app

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func flowFake() *clienttest.Fake {
	f := clienttest.New()
	f.ImageResolution = &client.ImageResolution{Reference: nextDigest, ExposedPorts: []int32{8080}}
	f.ClustersByName["europe"] = &client.Cluster{Name: "europe", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
	return f
}

func writes(fake *clienttest.Fake) []string {
	var result []string
	for _, call := range fake.Calls {
		if strings.HasPrefix(call, "Create") || strings.HasPrefix(call, "Update") || strings.HasPrefix(call, "Delete") {
			result = append(result, call)
		}
	}
	return result
}

func TestDeployFlowCreatesAndReusesAppEnvironment(t *testing.T) {
	fake := flowFake()
	out, review, err := run(factoryWith(fake), "deploy", "--image", "ghcr.io/acme/api:1.4", "--yes", "--wait=false", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var result deployResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.App != "api" || result.Environment != "production" || result.Cluster != "europe" || result.Image != nextDigest || result.Created != (deployCreated{App: true, Environment: true}) {
		t.Fatalf("result=%+v", result)
	}
	if len(fake.AppCreates) != 1 || len(fake.AppEnvironmentCreates) != 1 || fake.AppEnvironmentCreates[0].Web.Port != 8080 || !strings.Contains(review, "app: api (new)") {
		t.Fatalf("review=%s creates=%v", review, fake.AppEnvironmentCreates)
	}
	fake.ImageResolution.Reference = digest
	_, _, err = run(factoryWith(fake), "deploy", "--image", "ghcr.io/acme/api:1.5", "--yes", "--wait=false")
	if err != nil || len(fake.AppCreates) != 1 || len(fake.AppEnvironmentCreates) != 1 || len(fake.AppEnvironmentPatches) != 1 {
		t.Fatalf("rerun err=%v calls=%v", err, fake.Calls)
	}
}

func TestDeployFlowNoWriteBeforeReviewAndConfirmation(t *testing.T) {
	for _, mode := range []string{"dry-run", "no-confirm", "bad-port", "bad-key", "missing-port"} {
		t.Run(mode, func(t *testing.T) {
			fake := flowFake()
			args := []string{"deploy", "--image", nextDigest, "--wait=false"}
			switch mode {
			case "dry-run":
				args = append(args, "--dry-run")
			case "bad-port":
				args = append(args, "--port", "0", "--yes")
			case "bad-key":
				args = append(args, "--set-env", "not-an-assignment", "--yes")
			case "missing-port":
				fake.ImageResolution.ExposedPorts = nil
				args = append(args, "--yes")
			}
			out, review, err := run(factoryWith(fake), args...)
			if mode == "dry-run" {
				if err != nil || !strings.Contains(review, "Review deploy") {
					t.Fatalf("review=%s err=%v", review, err)
				}
			} else if cli.ExitCode(err) != cli.ExitMisuse {
				t.Fatalf("err=%v", err)
			}
			if len(writes(fake)) != 0 || out != "" {
				t.Fatalf("writes=%v out=%s", writes(fake), out)
			}
		})
	}
}

func TestDeployFlowClusterBranches(t *testing.T) {
	for _, tc := range []struct {
		name     string
		clusters []string
		flags    []string
		want     string
		create   bool
		misuse   bool
	}{
		{name: "none", misuse: true}, {name: "one", clusters: []string{"only"}, want: "only"}, {name: "many", clusters: []string{"a", "b"}, misuse: true},
		{name: "missing-name", clusters: []string{"a"}, flags: []string{"--cluster", "typo"}, misuse: true},
		{name: "create", flags: []string{"--cluster", "new", "--create-cluster"}, want: "new", create: true},
		{name: "default-create", flags: []string{"--create-cluster"}, want: "dev", create: true},
		{name: "reuse-default", clusters: []string{"dev"}, flags: []string{"--create-cluster"}, want: "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := flowFake()
			fake.ClustersByName = map[string]*client.Cluster{}
			for _, name := range tc.clusters {
				fake.ClustersByName[name] = &client.Cluster{Name: name, Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
			}
			args := append([]string{"deploy", "--image", nextDigest, "--dry-run"}, tc.flags...)
			_, review, err := run(factoryWith(fake), args...)
			if tc.misuse {
				if cli.ExitCode(err) != cli.ExitMisuse {
					t.Fatalf("err=%v", err)
				}
			} else {
				if err != nil || !strings.Contains(review, "cluster: "+tc.want) {
					t.Fatalf("err=%v review=%s", err, review)
				}
				if tc.create && !strings.Contains(review, tc.want+" (new)") {
					t.Fatal(review)
				}
			}
			if len(writes(fake)) != 0 {
				t.Fatal(fake.Calls)
			}
		})
	}
}

func TestDeployFlowExistingCreateFlagsAreRefused(t *testing.T) {
	for _, flags := range [][]string{{"--cpu", "1"}, {"--secret-env", "VAR=config:key"}, {"--secret", "config=apps/config"}, {"--domain", "api.example.test"}, {"--readiness", "tcp"}, {"--cluster", "other"}} {
		fake := flowFake()
		fake.AppsByName["shop"] = &client.App{Name: "shop"}
		fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{"production": releaseEnvironment()}
		args := append([]string{"deploy", "shop", "production", "--image", nextDigest, "--yes", "--wait=false"}, flags...)
		_, _, err := run(factoryWith(fake), args...)
		if cli.ExitCode(err) != cli.ExitMisuse || len(writes(fake)) != 0 {
			t.Fatalf("flags=%v err=%v calls=%v", flags, err, fake.Calls)
		}
	}
}

func TestDeployFlowDefaultsAndMultipleEnvironments(t *testing.T) {
	for _, tc := range []struct {
		count  int
		want   string
		misuse bool
	}{{0, "production", false}, {1, "staging", false}, {2, "", true}} {
		fake := flowFake()
		fake.AppsByName["shop"] = &client.App{Name: "shop"}
		fake.AppEnvironmentsByApp["shop"] = map[string]*client.AppEnvironment{}
		if tc.count > 0 {
			e := releaseEnvironment()
			e.Environment = "staging"
			fake.AppEnvironmentsByApp["shop"]["staging"] = e
		}
		if tc.count > 1 {
			fake.AppEnvironmentsByApp["shop"]["other"] = releaseEnvironment()
		}
		_, review, err := run(factoryWith(fake), "deploy", "shop", "--image", nextDigest, "--dry-run")
		if tc.misuse {
			if cli.ExitCode(err) != 2 {
				t.Fatal(err)
			}
		} else if err != nil || !strings.Contains(review, "environment: "+tc.want) {
			t.Fatalf("count=%d err=%v review=%s", tc.count, err, review)
		}
	}
}

func TestDeployFlowPortChoices(t *testing.T) {
	for _, ports := range [][]int32{nil, {80, 8080}} {
		fake := flowFake()
		fake.ImageResolution.ExposedPorts = ports
		_, _, err := run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false")
		if cli.ExitCode(err) != 2 || len(writes(fake)) != 0 {
			t.Fatalf("ports=%v err=%v", ports, err)
		}
		fake = flowFake()
		fake.ImageResolution.ExposedPorts = ports
		_, _, err = run(factoryWith(fake), "deploy", "--image", nextDigest, "--port", "9090", "--yes", "--wait=false")
		if err != nil || fake.AppEnvironmentCreates[0].Web.Port != 9090 {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestDeployFlowInteractiveDefaultsAndRefusal(t *testing.T) {
	fake := flowFake()
	streams, _, _ := cli.TestTTY()
	streams.PromptsEnabled = true
	streams.In = strings.NewReader("ghcr.io/acme/api:1.0\n\n\nno\n")
	f := factoryWith(fake)
	f.IOStreams = streams
	out, review, err := run(f, "deploy", "--wait=false")
	if cli.ExitCode(err) != 2 || len(writes(fake)) != 0 || out != "" || !strings.Contains(review, "App [api]") || !strings.Contains(review, "Environment [production]") {
		t.Fatalf("err=%v review=%s calls=%v", err, review, fake.Calls)
	}
	fake = flowFake()
	streams, _, _ = cli.TestTTY()
	streams.PromptsEnabled = true
	streams.In = strings.NewReader("ghcr.io/acme/api:1.0\n\n\nyes\n")
	f = factoryWith(fake)
	f.IOStreams = streams
	_, _, err = run(f, "deploy", "--wait=false")
	if err != nil || len(fake.AppCreates) != 1 || len(fake.AppEnvironmentCreates) != 1 {
		t.Fatalf("err=%v calls=%v", err, fake.Calls)
	}
}

func TestDeployFlowPartialFailureKeepsObjectsAndReruns(t *testing.T) {
	fake := flowFake()
	fake.CreateAppEnvironmentErr = errors.New("synthetic failure")
	_, review, err := run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false")
	if err == nil || len(fake.AppCreates) != 1 || !strings.Contains(review, "Created app hello-kupe") || !strings.Contains(review, "Resume: kupe app deploy hello-kupe production") {
		t.Fatalf("review=%s err=%v", review, err)
	}
	fake.CreateAppEnvironmentErr = nil
	_, _, err = run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false")
	if err != nil || len(fake.AppCreates) != 1 || len(fake.AppEnvironmentCreates) != 1 {
		t.Fatalf("err=%v calls=%v", err, fake.Calls)
	}
}

func TestDeployFlowClusterCreatedBeforeFailureIsReported(t *testing.T) {
	fake := flowFake()
	fake.ClustersByName = map[string]*client.Cluster{}
	fake.GetClusterSeq["new"] = []*client.Cluster{nil, {Name: "new", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}}
	fake.CreateAppErr = &client.APIError{StatusCode: 503, Message: "unavailable"}
	_, review, err := run(factoryWith(fake), "deploy", "--image", nextDigest, "--cluster", "new", "--create-cluster", "--yes", "--wait=false")
	if err == nil || !strings.Contains(review, "Created cluster new") || !strings.Contains(review, "--cluster new") {
		t.Fatalf("review=%s err=%v calls=%v", review, err, fake.Calls)
	}
}

func TestDeriveDeployAppName(t *testing.T) {
	for input, want := range map[string]string{"ghcr.io/acme/api:1.4": "api", "registry.example:5000/team/my_api:tag": "my-api", "ghcr.io/acme/API": "api", "ghcr.io/acme/a__b": "a-b"} {
		if got := deriveAppName(input); got != want {
			t.Fatalf("%s => %s", input, got)
		}
	}
}

func TestDeployFlowCompactResult(t *testing.T) {
	streams, out, _ := cli.Test()
	f := factoryWith(clienttest.New())
	f.IOStreams = streams
	format, _ := printer.Parse("json")
	p := &deployPresentation{created: deployCreated{App: true, Environment: true}}
	item := releaseEnvironment()
	item.Status.Address = "https://shop.example.test"
	if err := p.render(f, format, "shop", "production", item); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"app\": \"shop\",\n  \"environment\": \"production\",\n  \"cluster\": \"europe\",\n  \"release\": \"shop-production-1\",\n  \"image\": \"" + item.Web.Image + "\",\n  \"address\": \"https://shop.example.test\",\n  \"created\": {\n    \"app\": true,\n    \"environment\": true,\n    \"cluster\": false\n  }\n}\n"
	if out.String() != want {
		t.Fatalf("got=%s want=%s", out.String(), want)
	}
}

func TestDeployFlowCancellationBeforeInput(t *testing.T) {
	streams, _, _ := cli.TestTTY()
	streams.PromptsEnabled = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cli.PromptLine(ctx, streams, "Image", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDeployFlowRefusalsKeepExistingErrors(t *testing.T) {
	for _, tc := range []struct {
		name, code, message string
		status, exit        int
	}{
		{"apps", "apps_unavailable", "apps are not available on this platform", 404, cli.ExitUnavailable},
		{"image", "image_policy_denied", "image refused", 400, cli.ExitMisuse},
		{"payment", client.CodePaymentSetupRequired, "add a payment method", 409, cli.ExitConflict},
		{"plan", client.CodePlanLimitReached, "maximum cluster limit reached", 400, cli.ExitConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := flowFake()
			failure := &client.APIError{StatusCode: tc.status, Code: tc.code, Message: tc.message}
			args := []string{"deploy", "--image", nextDigest, "--yes", "--wait=false"}
			switch tc.name {
			case "apps":
				fake.GetAppErr = failure
			case "image":
				fake.ResolveAppImageErr = failure
			default:
				fake.CreateClusterErr = failure
				args = append(args, "--cluster", "new", "--create-cluster")
			}
			out, _, err := run(factoryWith(fake), args...)
			if cli.ExitCode(err) != tc.exit || out != "" || len(fake.AppCreates) != 0 || len(fake.AppEnvironmentCreates) != 0 {
				t.Fatalf("err=%v code=%d out=%s calls=%v", err, cli.ExitCode(err), out, fake.Calls)
			}
		})
	}
}

func TestDeployFlowResumePreservesFlagsWithoutLiteralValues(t *testing.T) {
	fake := flowFake()
	fake.CreateAppEnvironmentErr = errors.New("synthetic failure")
	_, notes, err := run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false", "--set-env", "TOKEN=never-print-this", "--port", "9090", "--cpu", "500m", "--domain", "api.example.test")
	if err == nil || strings.Contains(notes, "never-print-this") || !strings.Contains(notes, `--set-env "TOKEN=${TOKEN?set TOKEN before retrying}"`) || !strings.Contains(notes, "--port 9090") || !strings.Contains(notes, "--cpu 500m") || !strings.Contains(notes, "--domain api.example.test") || !strings.Contains(notes, "--wait=false") {
		t.Fatalf("err=%v notes=%s", err, notes)
	}
	if shellArgument("a'b $(bad)") != `'a'"'"'b $(bad)'` {
		t.Fatal("unsafe shell quoting")
	}
}

func TestDeployFlowUnchangedWaitUsesRequestedReleaseWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		phase, recovery string
		code            int
	}{{"Succeeded", "", 0}, {"Deploying", "", cli.ExitTimeout}, {"Failed", "", cli.ExitGeneral}, {"Failed", "Succeeded", cli.ExitGeneral}} {
		t.Run(tc.phase+tc.recovery, func(t *testing.T) {
			fake := releaseFake()
			env := fake.AppEnvironmentsByApp["shop"]["production"]
			release := recordedRelease(requestedRelease(env), tc.phase, 1, env.DeploymentRevision, env)
			if tc.recovery != "" {
				release.Status.Recovery = &client.AppReleaseRecovery{Phase: tc.recovery}
			}
			fake.AppReleases = []client.AppRelease{release}
			_, _, err := run(factoryWith(fake), "deploy", "shop", "production", "--image", env.Web.Image, "--yes", "--wait-timeout", "10ms")
			if cli.ExitCode(err) != tc.code || len(fake.AppEnvironmentPatches) != 0 || len(fake.AppEnvironmentCreates) != 0 {
				t.Fatalf("err=%v code=%d writes=%v", err, cli.ExitCode(err), writes(fake))
			}
		})
	}
}

type settlingDeployClient struct {
	client.Interface
	fake  *clienttest.Fake
	phase string
}

type settlingDeployEnvironments struct {
	client.AppEnvironmentAPI
	fake  *clienttest.Fake
	phase string
}

func (c settlingDeployClient) AppEnvironments(app string) client.AppEnvironmentAPI {
	return settlingDeployEnvironments{AppEnvironmentAPI: c.Interface.AppEnvironments(app), fake: c.fake, phase: c.phase}
}
func (a settlingDeployEnvironments) Create(ctx context.Context, req client.CreateAppEnvironmentRequest) (*client.AppEnvironment, string, error) {
	item, tag, err := a.AppEnvironmentAPI.Create(ctx, req)
	if err != nil {
		return item, tag, err
	}
	item.Status.RequestedRelease = &client.AppReleaseReference{Name: item.Name + "-1"}
	item.Status.Address = "https://api.example.test"
	a.fake.AppReleases = []client.AppRelease{recordedRelease(item.Name+"-1", a.phase, 1, item.DeploymentRevision, item)}
	return item, tag, nil
}

func TestDeployFlowCreatesWaitsAndPrintsOneAddress(t *testing.T) {
	for _, newCluster := range []bool{false, true} {
		fake := flowFake()
		f := factoryWith(fake)
		f.Client = func() (client.Interface, error) {
			return settlingDeployClient{Interface: fake, fake: fake, phase: "Succeeded"}, nil
		}
		args := []string{"deploy", "--image", "ghcr.io/acme/api:1.0", "--yes"}
		if newCluster {
			fake.ClustersByName = map[string]*client.Cluster{}
			fake.GetClusterSeq["new"] = []*client.Cluster{nil, {Name: "new", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}}
			args = append(args, "--cluster", "new", "--create-cluster")
		}
		out, notes, err := run(f, args...)
		if err != nil || out != "https://api.example.test\n" || strings.Count(notes, "waiting for release 1") != 1 {
			t.Fatalf("newCluster=%v err=%v out=%s notes=%s", newCluster, err, out, notes)
		}
		if newCluster {
			clusterReady, envCreate := -1, -1
			for i, call := range fake.Calls {
				if call == "GetCluster:new" {
					clusterReady = i
				}
				if call == "CreateAppEnvironment:production" {
					envCreate = i
				}
			}
			if fake.ClustersByName["new"] == nil || clusterReady < 0 || envCreate < clusterReady {
				t.Fatal(fake.Calls)
			}
		}
	}
}

func TestDeployFlowInteractiveClusterAndPortChoices(t *testing.T) {
	for _, tc := range []struct {
		clusters    []string
		input, want string
		create      bool
	}{
		{nil, "yes\n9090\nyes\n", "dev", true},
		{[]string{"west", "east"}, "2\n9090\nyes\n", "west", false},
	} {
		fake := flowFake()
		fake.ImageResolution.ExposedPorts = nil
		fake.ClustersByName = map[string]*client.Cluster{}
		for _, name := range tc.clusters {
			fake.ClustersByName[name] = &client.Cluster{Name: name, Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
		}
		if tc.create {
			fake.GetClusterSeq["dev"] = []*client.Cluster{{Name: "dev", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}}
		}
		streams, _, _ := cli.TestTTY()
		streams.PromptsEnabled = true
		streams.In = strings.NewReader(tc.input)
		f := factoryWith(fake)
		f.IOStreams = streams
		_, _, err := run(f, "deploy", "api", "production", "--image", nextDigest, "--wait=false")
		if err != nil || len(fake.AppEnvironmentCreates) != 1 || fake.AppEnvironmentCreates[0].Cluster != tc.want || fake.AppEnvironmentCreates[0].Web.Port != 9090 {
			t.Fatalf("err=%v calls=%v", err, fake.Calls)
		}
	}
}

func TestDeployFlowInteractiveRegistryAndEnvironmentChoices(t *testing.T) {
	fake := flowFake()
	fake.RegistriesByName["registry"] = &client.Registry{Name: "registry"}
	fake.Repositories = []client.Repository{{Name: "team/api", PullReference: "registry.example.test/acme/team/api"}}
	fake.Images["team/api"] = []client.Image{{Tags: []client.ImageTag{{Name: "1.4"}, {Name: "1.5"}}}}
	fake.AppsByName["api"] = &client.App{Name: "api"}
	staging := releaseEnvironment()
	staging.Environment = "staging"
	production := releaseEnvironment()
	fake.AppEnvironmentsByApp["api"] = map[string]*client.AppEnvironment{"staging": staging, "production": production}
	streams, _, _ := cli.TestTTY()
	streams.PromptsEnabled = true
	streams.In = strings.NewReader("\n2\n\n2\nyes\n")
	f := factoryWith(fake)
	f.IOStreams = streams
	_, notes, err := run(f, "deploy", "--wait=false")
	if err != nil || len(fake.ResolvedImages) != 1 || fake.ResolvedImages[0] != "registry.example.test/acme/team/api:1.5" || !strings.Contains(notes, "environment: staging (existing)") || len(fake.AppEnvironmentPatches) != 1 {
		t.Fatalf("err=%v notes=%s calls=%v", err, notes, fake.Calls)
	}
}

func TestDeployFlowResumeAllowsEmptyButRejectsUnsetVariables(t *testing.T) {
	fake := flowFake()
	fake.CreateAppEnvironmentErr = errors.New("synthetic failure")
	_, notes, _ := run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false", "--set-env", "TOKEN=")
	_, resume, ok := strings.Cut(notes, "Resume: ")
	if !ok {
		t.Fatal(notes)
	}
	for _, set := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		command := exec.CommandContext(ctx, "/bin/sh", "-c", "kupe() { printf '%s\\n' \"$@\"; }; "+strings.TrimSpace(resume))
		command.Env = []string{}
		if set {
			command.Env = []string{"TOKEN="}
		}
		out, err := command.Output()
		cancel()
		if set {
			if err != nil || !strings.Contains(string(out), "TOKEN=\n") {
				t.Fatalf("out=%s err=%v", out, err)
			}
		} else if err == nil || len(out) != 0 {
			t.Fatalf("unset out=%s err=%v", out, err)
		}
	}
}

func TestDeployFlowReviewUsesTruthfulAddressPreview(t *testing.T) {
	fake := flowFake()
	fake.TenantName = "acme"
	_, notes, err := run(factoryWith(fake), "deploy", "api", "production", "--image", nextDigest, "--dry-run")
	if err != nil || !strings.Contains(notes, "api-production.europe.acme.clusters (domain assigned by platform)") {
		t.Fatalf("err=%v notes=%s", err, notes)
	}
}
