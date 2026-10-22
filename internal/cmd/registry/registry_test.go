package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

func TestMain(m *testing.M) {
	issuePollInterval = time.Millisecond
	os.Exit(m.Run())
}

// testUser is the signed-in user of the test context.
const testUser = "tester@acme.com"

func factoryWith(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()
	io, _, _ := cli.Test()
	return factoryWithStreams(t, fake, io)
}

func factoryWithStreams(t *testing.T, fake *clienttest.Fake, io *cli.IOStreams) *cli.Factory {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext", User: testUser},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	f := cli.NewFactory(io, &cli.GlobalFlags{ConfigPath: cfgPath})
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

// run executes the registry command tree with args and returns its error
// with captured stdout and stderr.
func run(f *cli.Factory, args ...string) (stdout, stderr string, err error) {
	return runCtx(context.Background(), f, args...)
}

// runCtx is run with the command's context.
func runCtx(ctx context.Context, f *cli.Factory, args ...string) (stdout, stderr string, err error) {
	cmd := NewCmd(f)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err = cmd.Execute()
	return f.IOStreams.Out.(interface{ String() string }).String(),
		f.IOStreams.ErrOut.(interface{ String() string }).String(), err
}

// interactive turns prompts on and feeds answer to stdin.
func interactive(f *cli.Factory, answer string) {
	f.IOStreams.PromptsEnabled = true
	f.IOStreams.In = strings.NewReader(answer)
}

func hint(err error) string {
	var e *cli.Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}

func assertExit(t *testing.T, err error, want int) {
	t.Helper()
	if code := cli.ExitCode(err); code != want {
		t.Fatalf("exit = %d; want %d (err: %v)", code, want, err)
	}
}

func hasCall(fake *clienttest.Fake, call string) bool {
	for _, c := range fake.Calls {
		if c == call {
			return true
		}
	}
	return false
}

func assertNoCall(t *testing.T, fake *clienttest.Fake, prefix string) {
	t.Helper()
	for _, c := range fake.Calls {
		if strings.HasPrefix(c, prefix) {
			t.Fatalf("unexpected call %q in %v", c, fake.Calls)
		}
	}
}

const digest = "sha256:4f8c0d0e4b1f2b6e7e0c6a3d6c0f3a9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a"

func runningRegistry() *client.Registry {
	limit := int64(5 << 30)
	return &client.Registry{
		Name: client.RegistryName, DeletionProtection: true, CreatedAt: "2026-10-21T10:00:00Z",
		Status: &client.RegistryStatus{Phase: client.PhaseRunning, Endpoint: "registry.kupe.cloud/acme",
			StorageUsedBytes: 1288490188, StorageLimitBytes: &limit, RepositoryCount: 2},
	}
}

// seeded is a fake with a running registry, a connection, a repository
// with two images and a token.
func seeded() *clienttest.Fake {
	fake := clienttest.New()
	fake.RegistriesByName[client.RegistryName] = runningRegistry()
	fake.RegistryBindingsByName["prod"] = &client.RegistryBinding{Name: "prod", Registry: "registry", Cluster: "prod",
		PushNamespaces: []string{"ci"}, SecretName: "kupe-registry-push",
		Status: &client.RegistryBindingStatus{Phase: client.PhaseReady, PullUsername: "kupe+acme+pull-prod",
			Namespaces: []client.RegistryBindingNamespaceStatus{{Name: "ci", Phase: client.PhaseReady}},
			Rotation:   &client.RegistryBindingRotationStatus{}}}
	fake.Repositories = []client.Repository{{Name: "team-a/api", ImageCount: 2}, {Name: "web", ImageCount: 1}}
	fake.Images["team-a/api"] = []client.Image{
		{Repository: "team-a/api", Digest: digest, Tags: []client.ImageTag{{Name: "v1"}}},
		{Repository: "team-a/api", Digest: "sha256:" + strings.Repeat("b", 64)},
	}
	fake.Images["web"] = []client.Image{}
	fake.RegistryTokens["rt-00000000000000aa"] = &client.RegistryToken{ID: "rt-00000000000000aa", Kind: client.RegistryTokenKindToken,
		DisplayName: "ci", Access: client.RegistryAccessPull, Username: "kupe+acme+tok-00000000000000aa",
		Status: &client.RegistryTokenStatus{Phase: client.PhaseReady}}
	return fake
}

// everyCommand is one invocation of each registry command that reaches the
// API, against seeded().
// registryArgCalls are the fake's calls that record the registry argument
// first.
var registryArgCalls = map[string]bool{
	"RegistryBindings": true, "RotateRegistryBinding": true, "ListRepositories": true, "GetRepository": true,
	"DeleteRepository": true, "ListImages": true, "GetImage": true, "DeleteImage": true, "DeleteTag": true,
	"ListRegistryTokens": true, "GetRegistryToken": true, "CreateRegistryToken": true, "DeleteRegistryToken": true,
	"CreateRegistrySession": true,
}

var everyCommand = [][]string{
	{"get"}, {"list"}, {"wait", "--for", "ready", "--timeout", "5s"},
	{"update", "--deletion-protection=false"},
	{"connect", "staging", "--wait=false"}, {"connection", "list"}, {"connection", "get", "prod"},
	{"connection", "update", "prod", "--add-push-namespace", "release", "--wait=false"},
	{"connection", "rotate", "prod", "--yes", "--wait=false"},
	{"disconnect", "prod", "--yes"},
	{"repo", "list"}, {"repo", "get", "team-a/api"}, {"repo", "delete", "web", "--yes"},
	{"image", "list", "team-a/api"}, {"image", "get", "team-a/api:v1"},
	{"image", "delete", "team-a/api@sha256:" + strings.Repeat("b", 64), "--yes"},
	{"tag", "delete", "team-a/api:v1", "--yes"},
	{"token", "list"}, {"token", "get", "rt-00000000000000aa"}, {"token", "create", "--name", "ci", "--access", "pull"},
	{"token", "revoke", "rt-00000000000000aa", "--yes"},
	{"login", "--no-store", "-o", "json"}, {"logout", "--docker-config", "DIR"},
	{"delete", "--yes", "--wait-timeout", "5s"},
}

func TestRegistryName_Fixed(t *testing.T) {
	t.Run("every request addresses the one registry", func(t *testing.T) {
		for _, args := range everyCommand {
			fake := seeded()
			fake.RegistriesByName[client.RegistryName].DeletionProtection = false
			f := factoryWith(t, fake)
			for i, a := range args {
				if a == "DIR" {
					args[i] = t.TempDir()
				}
			}
			if _, _, err := run(f, args...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			addressed := 0
			for _, call := range fake.Calls {
				name, rest, _ := strings.Cut(call, ":")
				registry, _, _ := strings.Cut(rest, ":")
				switch {
				case name == "ListRegistrys":
					addressed++ // the collection, which holds only the one
					continue
				case registryArgCalls[name]:
				case strings.HasSuffix(name, "Registry") && name != "RotateRegistry":
					// The registry collection's own calls name the resource.
				default:
					continue
				}
				addressed++
				if registry != client.RegistryName {
					t.Errorf("%v: call %q addresses registry %q", args, call, registry)
				}
			}
			if addressed == 0 {
				t.Errorf("%v made no registry call: %v", args, fake.Calls)
			}
		}
	})
	t.Run("no command takes a registry name or --registry", func(t *testing.T) {
		f := factoryWith(t, clienttest.New())
		var walk func(*cobra.Command)
		walk = func(c *cobra.Command) {
			c.Flags().VisitAll(func(fl *pflag.Flag) {
				if fl.Name == "registry" {
					t.Errorf("%s has a --registry flag", c.CommandPath())
				}
			})
			for _, sub := range c.Commands() {
				walk(sub)
			}
		}
		root := NewCmd(f)
		walk(root)
		for _, verb := range []string{"create", "get", "list", "update", "delete", "wait", "login", "logout"} {
			cmd, _, err := root.Find([]string{verb})
			if err != nil {
				t.Fatal(err)
			}
			if cmd.Args == nil || cmd.Args(cmd, []string{"registry"}) == nil {
				t.Errorf("%s accepts a name argument", verb)
			}
		}
	})
	t.Run("a 404 on the registry says how to create one", func(t *testing.T) {
		for _, args := range [][]string{{"get"}, {"update", "--deletion-protection"}, {"repo", "list"}, {"token", "list"}, {"connect", "prod"}} {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			_, _, err := run(f, args...)
			assertExit(t, err, cli.ExitNotFound)
			if !strings.Contains(hint(err), "kupe registry create") {
				t.Errorf("%v: hint = %q", args, hint(err))
			}
		}
	})
	// F-1 (SA): a switched-off platform answers registry reads with a 404
	// that carries registries_unavailable; that is a platform setting, not
	// a missing registry, so it keeps its own hint and never says create.
	t.Run("registries_unavailable on a registry read keeps its hint", func(t *testing.T) {
		off := &client.APIError{StatusCode: 404, Code: client.RegistryCodeUnavailable, Message: "registries are not available on this platform"}
		for _, args := range [][]string{
			{"get"}, {"update", "--deletion-protection"}, {"delete", "--yes"},
			{"wait", "--for", "ready", "--timeout", "5s"}, {"connect", "prod"},
		} {
			fake := seeded()
			fake.GetRegistryErr, fake.UpdateRegistryErr, fake.DeleteRegistryErr = off, off, off
			_, _, err := run(factoryWith(t, fake), args...)
			assertExit(t, err, cli.ExitNotFound)
			if !strings.Contains(hint(err), "not an outage") || strings.Contains(hint(err), "kupe registry create") {
				t.Errorf("%v: err = %v, hint = %q", args, err, hint(err))
			}
		}
	})
	t.Run("a 404 on a repository names it, without the create hint", func(t *testing.T) {
		f := factoryWith(t, seeded())
		_, _, err := run(f, "repo", "get", "nothere")
		assertExit(t, err, cli.ExitNotFound)
		if !strings.Contains(err.Error(), `repository "nothere" not found`) || strings.Contains(hint(err), "create") {
			t.Fatalf("err = %v, hint %q", err, hint(err))
		}
	})
}

func TestCreate_DeletionProtection(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want *bool
	}{
		{"flag absent sends no field", nil, nil},
		{"--no-deletion-protection sends false", []string{"--no-deletion-protection"}, new(bool)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			if _, _, err := run(f, append([]string{"create", "--wait=false"}, tt.args...)...); err != nil {
				t.Fatal(err)
			}
			req := fake.RegistryCreates[0]
			if req.Name != client.RegistryName {
				t.Fatalf("name = %q", req.Name)
			}
			if (req.DeletionProtection == nil) != (tt.want == nil) || (req.DeletionProtection != nil && *req.DeletionProtection != *tt.want) {
				t.Fatalf("deletionProtection = %v; want %v", req.DeletionProtection, tt.want)
			}
		})
	}
}

func TestCreate_StorageLimitAndNextSteps(t *testing.T) {
	fake := clienttest.New()
	fake.GetRegistrySeq[client.RegistryName] = []*client.Registry{
		{Name: "registry", Status: &client.RegistryStatus{Phase: client.PhaseProvisioning}},
		runningRegistry(),
	}
	f := factoryWith(t, fake)
	out, errOut, err := run(f, "create", "--storage-limit", "5Gi")
	if err != nil {
		t.Fatal(err)
	}
	if fake.RegistryCreates[0].StorageLimit != "5Gi" {
		t.Fatalf("storageLimit = %q", fake.RegistryCreates[0].StorageLimit)
	}
	if !strings.Contains(out, "Running") || !strings.Contains(errOut, "registry.kupe.cloud/acme/REPOSITORY:TAG") ||
		!strings.Contains(errOut, "kupe registry login") || !strings.Contains(errOut, "kupe registry connect CLUSTER") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", out, errOut)
	}
	for _, bad := range []string{"5", "5G", "0Gi", "5gi", "1234567890123456789Gi", "1.5Ki"} {
		_, _, err := run(factoryWith(t, clienttest.New()), "create", "--storage-limit", bad)
		assertExit(t, err, cli.ExitMisuse)
	}
}

func TestUpdate_DeletionProtection(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	if _, _, err := run(f, "update", "--deletion-protection=false"); err != nil {
		t.Fatal(err)
	}
	p := fake.RegistryPatches[0]
	if p.DeletionProtection == nil || *p.DeletionProtection || p.StorageLimit != nil {
		t.Fatalf("patch = %+v", p)
	}
	_, _, err := run(factoryWith(t, seeded()), "update")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(err.Error(), "nothing to update: pass at least one of --storage-limit, --clear-storage-limit, --deletion-protection") {
		t.Fatalf("err = %v", err)
	}
	fake = seeded()
	if _, _, err := run(factoryWith(t, fake), "update", "--clear-storage-limit"); err != nil {
		t.Fatal(err)
	}
	if p := fake.RegistryPatches[0]; p.StorageLimit == nil || *p.StorageLimit != "" || p.DeletionProtection != nil {
		t.Fatalf("clear patch = %+v", p)
	}
	_, _, err = run(factoryWith(t, seeded()), "update", "--storage-limit", "1Gi", "--clear-storage-limit")
	assertExit(t, err, cli.ExitMisuse)
}

func TestRegistryDelete_Protected(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	_, _, err := run(f, "delete", "--yes")
	assertExit(t, err, cli.ExitConflict)
	if hint(err) != "turn deletion protection off first:  kupe registry update --deletion-protection=false" {
		t.Fatalf("hint = %q", hint(err))
	}
	if _, ok := fake.RegistriesByName[client.RegistryName]; !ok {
		t.Fatal("a protected registry was deleted")
	}
}

func TestRegistryDelete_WarningAndConfirmByName(t *testing.T) {
	fake := seeded()
	fake.RegistriesByName[client.RegistryName].DeletionProtection = false
	f := factoryWith(t, fake)
	interactive(f, "yes\n")
	_, errOut, err := run(f, "delete")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(errOut, "Deleting a registry deletes every image in it. They cannot be restored.") ||
		!strings.Contains(errOut, `Type the name to confirm`) {
		t.Fatalf("stderr = %q", errOut)
	}
	assertNoCall(t, fake, "DeleteRegistry")

	interactive(f, "registry\n")
	if _, _, err := run(f, "delete", "--wait-timeout", "5s"); err != nil {
		t.Fatal(err)
	}
	if !hasCall(fake, "DeleteRegistry:registry") {
		t.Fatalf("calls = %v", fake.Calls)
	}
}

func TestWait_ReadyMeansRunning(t *testing.T) {
	// F-3: ready is case-insensitive, like Running and Deleted.
	for _, phase := range []string{"ready", "Ready", "READY", "Running"} {
		fake := seeded()
		f := factoryWith(t, fake)
		out, _, err := run(f, "wait", "--for", phase, "--timeout", "5s")
		if err != nil || out != "registry/registry running\n" {
			t.Fatalf("--for %s: out = %q, err = %v", phase, out, err)
		}
	}
}

func TestRegistryErrors(t *testing.T) {
	api := func(status int, code, msg string) error {
		return &client.APIError{StatusCode: status, Code: code, Message: msg}
	}
	tests := []struct {
		name     string
		err      error
		cluster  string
		wantExit int
		wantHint string
	}{
		{"switched off", api(404, "registries_unavailable", "registries are not available on this platform"), "", cli.ExitNotFound, "not an outage"},
		{"deletion protected", api(409, "deletion_protected", "x"), "", cli.ExitConflict, "kupe registry update --deletion-protection=false"},
		{"session needs a user", api(403, "session_requires_user", "x"), "", cli.ExitAuth, "kupe auth login"},
		{"already exists", api(409, "REGISTRY_ALREADY_EXISTS", "x"), "", cli.ExitConflict, "kupe registry get"},
		{"connection exists", api(409, "REGISTRY_BINDING_EXISTS", "x"), "prod", cli.ExitConflict, "kupe registry connection get prod"},
		{"push namespaces over 10", api(400, "REGISTRY_PUSH_NAMESPACES_LIMIT", "x"), "", cli.ExitMisuse, ""},
		{"storage above plan", api(400, "REGISTRY_STORAGE_LIMIT_ABOVE_PLAN", "x"), "", cli.ExitMisuse, ""},
		{"expiry invalid", api(400, "REGISTRY_TOKEN_EXPIRY_INVALID", "x"), "", cli.ExitMisuse, ""},
		{"push namespaces over 20", api(400, "REGISTRY_PUSH_NAMESPACES_SERVICE_LIMIT", "x"), "", cli.ExitConflict, "at most 20 namespaces across all its connections"},
		{"push token admin only", api(403, "REGISTRY_TOKEN_PUSH_ADMIN_ONLY", ""), "", cli.ExitAuth, "--access pull"},
		{"token limit", api(409, "REGISTRY_TOKEN_LIMIT_REACHED", "x"), "", cli.ExitConflict, "kupe registry token list"},
		{"image in use", api(409, "image_in_use", "x"), "", cli.ExitConflict, "delete the multi-platform image that contains it"},
		{"image protected", api(409, "image_protected", "x"), "", cli.ExitConflict, ""},
		{"not ready", api(409, "registry_not_ready", "x"), "", cli.ExitConflict, "kupe registry wait --for ready"},
		{"registry terminating", api(409, "terminating", "x"), "", cli.ExitConflict, "kupe registry get"},
		{"connection terminating", api(409, "terminating", "x"), "prod", cli.ExitConflict, "kupe registry connection get prod"},
		{"no registry", api(404, "", `registry "registry" not found`), "", cli.ExitNotFound, "kupe registry create"},
		{"plan has no registry", api(400, "plan_limit_reached", "x"), "", cli.ExitConflict, "upgrade"},
		{"tenant suspended (shared)", api(409, "tenant_suspended", "x"), "", cli.ExitConflict, "contact support"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := translateErr(tt.err, tt.cluster)
			assertExit(t, err, tt.wantExit)
			if !strings.Contains(hint(err), tt.wantHint) || (tt.wantHint == "" && hint(err) != "") {
				t.Fatalf("hint = %q; want %q", hint(err), tt.wantHint)
			}
			if !client.IsAPIError(err) {
				t.Fatal("the API error must stay in the chain")
			}
		})
	}
	// The CLI's own sentence shows only when the API sent no message.
	if got := translateErr(api(403, "REGISTRY_TOKEN_PUSH_ADMIN_ONLY", ""), "").Error(); !strings.HasPrefix(got, "only tenant admins can create push access tokens") {
		t.Fatalf("fallback = %q", got)
	}
}
