package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

const testTokenID = "cat-0123456789abcdef"

var testToken = clienttest.FakeClusterAccessTokenSecret(testTokenID)

// fastAccessTokenWait shrinks the poll interval for the test.
func fastAccessTokenWait(t *testing.T) {
	t.Helper()
	prev := accessTokenPollInterval
	accessTokenPollInterval = time.Millisecond
	t.Cleanup(func() { accessTokenPollInterval = prev })
}

// runAT runs "kupe cluster access-tokens ARGS..." and returns stdout,
// stderr and the error.
func runAT(ctx context.Context, f *cli.Factory, args ...string) (stdout, stderr string, err error) {
	cmd := NewCmd(f)
	cmd.SetContext(ctx)
	cmd.SetArgs(append([]string{"access-tokens"}, args...))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err = cmd.Execute()
	return f.IOStreams.Out.(interface{ String() string }).String(),
		f.IOStreams.ErrOut.(interface{ String() string }).String(), err
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if code := cli.ExitCode(err); code != want {
		t.Fatalf("exit = %d; want %d (err: %v)", code, want, err)
	}
}

func calls(fake *clienttest.Fake, prefix string) int {
	n := 0
	for _, c := range fake.Calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// readyAfter reports the stored token Pending for its first n polls.
type readyAfter struct {
	*clienttest.Fake
	mu    sync.Mutex
	polls int
	n     int
}

func (r *readyAfter) GetClusterAccessToken(ctx context.Context, cluster, id string) (*client.ClusterAccessToken, error) {
	r.mu.Lock()
	r.polls++
	early := r.polls <= r.n
	r.mu.Unlock()
	t, err := r.Fake.GetClusterAccessToken(ctx, cluster, id)
	if err == nil && early {
		t.Status = &client.ClusterAccessTokenStatus{Phase: client.PhasePending}
	}
	return t, err
}

// withClient makes f hand out api instead of the fake.
func withClient(f *cli.Factory, api client.Interface) *cli.Factory {
	f.Client = func() (client.Interface, error) { return api, nil }
	return f
}

func TestAccessTokenCreate_FlagMisuse(t *testing.T) {
	for _, args := range [][]string{
		{"dev"},
		{"dev", "--name", "  "},
		{"dev", "--name", strings.Repeat("a", 129)},
		{"dev", "--name", "ci\x07"},
		{"dev", "--name", "ci", "--role", "readonly"},
		{"dev", "--name", "ci", "--expires-in-days", "0"},
		{"dev", "--name", "ci", "--expires-in-days", "366"},
		{"dev", "--name", "ci", "-o", "json", "--merge"},
		{"dev", "--name", "ci", "-o", "yaml"},
		{"dev", "--name", "ci", "--context-name", "x"},
		{"dev", "--name", "ci", "--wait-timeout", "0s"},
		{"dev", "--name", "ci", "--wait-timeout", "-1s"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			fake := clienttest.New()
			_, _, err := runAT(context.Background(), factoryWith(t, fake), append([]string{"create"}, args...)...)
			assertExitCode(t, err, cli.ExitMisuse)
			if n := calls(fake, "CreateClusterAccessToken"); n != 0 {
				t.Fatalf("created a token on misuse: %v", fake.Calls)
			}
		})
	}
	_, _, err := runAT(context.Background(), factoryWith(t, clienttest.New()), "create", "dev", "--name", "ci", "--role", "readonly")
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Hint != "v1 tokens are admin only" {
		t.Fatalf("--role readonly: err = %v; want the admin-only hint", err)
	}
}

func TestAccessTokenCreate_WaitsThenPrintsOnlyTheKubeconfig(t *testing.T) {
	fastAccessTokenWait(t)
	fake := clienttest.New()
	api := &readyAfter{Fake: fake, n: 2}
	f := withClient(factoryWith(t, fake), api)
	out, errOut, err := runAT(context.Background(), f, "create", "dev", "--name", "ci")
	if err != nil {
		t.Fatal(err)
	}
	if errOut != "" {
		t.Fatalf("stderr should be empty off a TTY, got %q", errOut)
	}
	cfg, err := clientcmd.Load([]byte(out))
	if err != nil {
		t.Fatalf("stdout is not a kubeconfig: %v\n%s", err, out)
	}
	want := "kupe-acme-dev-" + testTokenID
	if cfg.CurrentContext != want || cfg.AuthInfos[want] == nil || cfg.AuthInfos[want].Token != testToken {
		t.Fatalf("kubeconfig = %+v", cfg)
	}
	if strings.Count(out, "apiVersion: v1") != 1 || !strings.HasPrefix(out, "apiVersion: v1") {
		t.Fatalf("stdout holds more than the kubeconfig:\n%s", out)
	}
	if api.polls < 3 || calls(fake, "CollectClusterAccessToken") != 1 {
		t.Fatalf("polls = %d, calls = %v; want two Pending polls, then one collect", api.polls, fake.Calls)
	}
	body := fake.ClusterAccessTokenCreates[0]
	if body.DisplayName != "ci" || body.Access.Role != "admin" || body.ExpiresInDays != nil {
		t.Fatalf("create body = %+v", body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "expiresInDays") {
		t.Fatalf("expiresInDays sent without the flag: %s", raw)
	}
}

func TestAccessTokenCreate_ExpiresInDaysSent(t *testing.T) {
	fake := clienttest.New()
	if _, _, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci", "--expires-in-days", "30"); err != nil {
		t.Fatal(err)
	}
	if d := fake.ClusterAccessTokenCreates[0].ExpiresInDays; d == nil || *d != 30 {
		t.Fatalf("expiresInDays = %v; want 30", d)
	}
}

func TestAccessTokenCreate_TTYDetailsOnStderr(t *testing.T) {
	fake := clienttest.New()
	io, _, _ := cli.TestTTY()
	f := factoryWith(t, fake)
	f.IOStreams = io
	out, errOut, err := runAT(context.Background(), f, "create", "dev", "--name", "ci")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Store this kubeconfig now: it is shown only once.",
		testTokenID,
		"2027-02-20T10:00:00Z",
		"Revoke with: kupe cluster access-tokens revoke dev " + testTokenID,
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, testToken) {
		t.Fatal("the token reached stderr")
	}
	if !strings.Contains(out, testToken) {
		t.Fatalf("stdout = %q", out)
	}
}

func TestAccessTokenCreate_JSONIncludesTheToken(t *testing.T) {
	fake := clienttest.New()
	out, _, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for key, want := range map[string]any{
		"id": testTokenID, "cluster": "dev", "name": "ci", "role": "admin", "createdBy": "tester@acme.com",
		"createdAt": "2026-11-22T10:00:00Z", "expiresAt": "2027-02-20T10:00:00Z", "token": testToken,
	} {
		if got[key] != want {
			t.Errorf("%s = %v; want %v", key, got[key], want)
		}
	}
	if kc, _ := got["kubeconfig"].(string); !strings.Contains(kc, "token: "+testToken) {
		t.Errorf("kubeconfig = %q", kc)
	}
}

func TestAccessTokenCreate_FailuresRevoke(t *testing.T) {
	fastAccessTokenWait(t)
	notReady := &client.APIError{StatusCode: 409, Code: "ACCESS_TOKEN_NOT_READY", Message: "the access token is not ready yet; try again in a few seconds"}
	gone := &client.APIError{StatusCode: 410, Code: "ACCESS_TOKEN_ALREADY_COLLECTED", Message: "the access token was already collected"}
	for _, tt := range []struct {
		name    string
		phase   string
		collect []error
		args    []string
		exit    int
		msg     string
	}{
		{name: "failed", phase: client.PhaseFailed, exit: cli.ExitGeneral, msg: "the cluster could not issue the access token; it was revoked"},
		{name: "timeout", phase: client.PhasePending, args: []string{"--wait-timeout", "30ms"}, exit: cli.ExitTimeout,
			msg: "the cluster did not issue the access token in time; it was revoked"},
		{name: "collect keeps answering not ready", collect: repeat(notReady, 1000), args: []string{"--wait-timeout", "50ms"}, exit: cli.ExitTimeout,
			msg: "the cluster did not issue the access token in time; it was revoked"},
		{name: "already collected", collect: []error{gone}, exit: cli.ExitConflict,
			msg: "the access token was already collected elsewhere; it was revoked. Create another."},
		{name: "collect response lost, then 410", collect: []error{errors.New("executing request: connection reset by peer"), gone}, exit: cli.ExitConflict,
			msg: "the access token could not be delivered and was revoked"},
		{name: "collect 500 after the claim, then 410", collect: []error{&client.APIError{StatusCode: 500, Message: "internal server error"}, gone}, exit: cli.ExitConflict,
			msg: "the access token could not be delivered and was revoked"},
		{name: "creator only", collect: []error{&client.APIError{StatusCode: 403, Code: "ACCESS_TOKEN_COLLECT_CREATOR_ONLY", Message: "only the member or API key that created an access token can collect it"}},
			exit: cli.ExitAuth, msg: "it was revoked"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.CreatedClusterAccessTokenPhase = tt.phase
			fake.CollectClusterAccessTokenErrs = tt.collect
			out, _, err := runAT(context.Background(), factoryWith(t, fake), append([]string{"create", "dev", "--name", "ci"}, tt.args...)...)
			assertExitCode(t, err, tt.exit)
			if !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("err = %v; want %q", err, tt.msg)
			}
			if out != "" {
				t.Fatalf("stdout = %q; want nothing for a token that was not handed over", out)
			}
			if _, ok := fake.ClusterAccessTokens[testTokenID]; ok {
				t.Fatal("the token was not revoked")
			}
		})
	}
}

func repeat(err error, n int) []error {
	out := make([]error, n)
	for i := range out {
		out[i] = err
	}
	return out
}

func TestAccessTokenCreate_CollectNotReadyThenOK(t *testing.T) {
	fastAccessTokenWait(t)
	fake := clienttest.New()
	fake.CollectClusterAccessTokenErrs = []error{
		&client.APIError{StatusCode: 409, Code: "ACCESS_TOKEN_NOT_READY", Message: "the access token is not ready yet; try again in a few seconds"},
		&client.APIError{StatusCode: 503, Message: "cluster is not yet ready"},
	}
	out, _, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, testToken) || calls(fake, "CollectClusterAccessToken") != 3 {
		t.Fatalf("stdout = %q, calls = %v", out, fake.Calls)
	}
	if _, ok := fake.ClusterAccessTokens[testTokenID]; !ok {
		t.Fatal("a collected token was revoked")
	}
}

// cancelOnPoll is the fake with Ctrl+C pressed during the wait.
type cancelOnPoll struct {
	*clienttest.Fake
	cancel context.CancelFunc
}

func (c cancelOnPoll) GetClusterAccessToken(ctx context.Context, cluster, id string) (*client.ClusterAccessToken, error) {
	c.cancel()
	return c.Fake.GetClusterAccessToken(ctx, cluster, id)
}

func TestAccessTokenCreate_InterruptRevokes(t *testing.T) {
	fake := clienttest.New()
	fake.CreatedClusterAccessTokenPhase = client.PhasePending
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := withClient(factoryWith(t, fake), cancelOnPoll{Fake: fake, cancel: cancel})
	out, _, err := runAT(ctx, f, "create", "dev", "--name", "ci")
	assertExitCode(t, err, cli.ExitInterrupted)
	if !strings.Contains(err.Error(), "stopped waiting for the access token; it was revoked") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := fake.ClusterAccessTokens[testTokenID]; ok {
		t.Fatal("the interrupted token was not revoked")
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestAccessTokenCreate_FailedRevokePrintsTheCommand(t *testing.T) {
	fastAccessTokenWait(t)
	fake := clienttest.New()
	fake.CreatedClusterAccessTokenPhase = client.PhaseFailed
	fake.DeleteClusterAccessTokenErr = &client.APIError{StatusCode: 500, Message: "boom"}
	_, _, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci")
	assertExitCode(t, err, cli.ExitGeneral)
	want := "revoke it with:  kupe cluster access-tokens revoke dev " + testTokenID + " --yes"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v; want %q", err, want)
	}
}

func TestAccessTokenCreate_Merge(t *testing.T) {
	kcPath := filepath.Join(t.TempDir(), "config")
	t.Setenv("KUBECONFIG", kcPath)
	fake := clienttest.New()
	out, errOut, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci", "--merge")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("--merge printed to stdout: %q", out)
	}
	if strings.Contains(errOut, testToken) {
		t.Fatal("the token reached stderr")
	}
	name := "kupe-acme-dev-" + testTokenID
	if !strings.Contains(errOut, fmt.Sprintf("Merged %q", name)) || !strings.Contains(errOut, "kubectl --context "+name) {
		t.Fatalf("stderr = %q", errOut)
	}
	cfg, err := clientcmd.LoadFromFile(kcPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != name || cfg.AuthInfos[name] == nil || cfg.AuthInfos[name].Token != testToken ||
		cfg.Clusters[name] == nil || cfg.Clusters[name].Server != clienttest.FakeClusterEndpoint || len(cfg.Clusters[name].CertificateAuthorityData) == 0 {
		t.Fatalf("merged kubeconfig = %+v", cfg)
	}
	// The exec context "kupe cluster kubeconfig --merge" writes is never the
	// token's.
	if cfg.Contexts["kupe-acme-dev"] != nil {
		t.Fatal("wrote the exec kubeconfig's context name")
	}
}

func TestAccessTokenCreate_MergeCollisionRefusedBeforeCreate(t *testing.T) {
	kcPath := filepath.Join(t.TempDir(), "config")
	t.Setenv("KUBECONFIG", kcPath)
	seed := clientcmdapi.NewConfig()
	seed.Clusters["ci"] = &clientcmdapi.Cluster{Server: "https://other:6443"}
	seed.AuthInfos["ci"] = &clientcmdapi.AuthInfo{Token: "other"}
	seed.Contexts["ci"] = &clientcmdapi.Context{Cluster: "ci", AuthInfo: "ci"}
	if err := clientcmd.WriteToFile(*seed, kcPath); err != nil {
		t.Fatal(err)
	}
	fake := clienttest.New()
	_, _, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci", "--merge", "--context-name", "ci")
	assertExitCode(t, err, cli.ExitConflict)
	if calls(fake, "CreateClusterAccessToken") != 0 {
		t.Fatalf("created a token that could not be merged: %v", fake.Calls)
	}

	// --force overwrites it.
	fake = clienttest.New()
	if _, _, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci", "--merge", "--context-name", "ci", "--force"); err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.LoadFromFile(kcPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthInfos["ci"].Token != testToken {
		t.Fatalf("--force did not overwrite: %+v", cfg.AuthInfos["ci"])
	}
}

func TestAccessTokenCreate_MergeWriteFailureRevokes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", filepath.Join(dir, "config"))
	fake := clienttest.New()
	_, errOut, err := runAT(context.Background(), factoryWith(t, fake), "create", "dev", "--name", "ci", "--merge")
	if err == nil || !strings.Contains(err.Error(), "the kubeconfig could not be written, so the access token was lost; it was revoked") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error()+errOut, testToken) {
		t.Fatal("the token reached the error")
	}
	if _, ok := fake.ClusterAccessTokens[testTokenID]; ok {
		t.Fatal("the lost token was not revoked")
	}
}

func TestAccessTokenListGetRevoke(t *testing.T) {
	seed := func() *clienttest.Fake {
		fake := clienttest.New()
		fake.ClusterAccessTokens[testTokenID] = &client.ClusterAccessToken{
			ID: testTokenID, Cluster: "dev", DisplayName: "ci", Access: client.ClusterAccessTokenAccess{Role: "admin"},
			CreatedBy: "alice@acme.com", ExpiresAt: "2027-02-20T10:00:00Z", Collected: true,
			Status: &client.ClusterAccessTokenStatus{Phase: client.PhaseReady},
		}
		return fake
	}
	fake := seed()
	out, _, err := runAT(context.Background(), factoryWith(t, fake), "list", "dev")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CREATED-BY", testTokenID, "alice@acme.com", "2027-02-20", "Collected"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}

	out, _, err = runAT(context.Background(), factoryWith(t, seed()), "get", "dev", testTokenID, "-o", "json")
	if err != nil || !strings.Contains(out, `"id": "`+testTokenID+`"`) {
		t.Fatalf("get: %v\n%s", err, out)
	}

	_, _, err = runAT(context.Background(), factoryWith(t, seed()), "get", "prod", testTokenID)
	assertExitCode(t, err, cli.ExitNotFound)

	_, _, err = runAT(context.Background(), factoryWith(t, seed()), "revoke", "dev", testTokenID)
	assertExitCode(t, err, cli.ExitMisuse)

	fake = seed()
	out, _, err = runAT(context.Background(), factoryWith(t, fake), "revoke", "dev", testTokenID, "--yes")
	if err != nil || out != "access-token/"+testTokenID+" revoked\n" {
		t.Fatalf("revoke: %v %q", err, out)
	}
	if _, ok := fake.ClusterAccessTokens[testTokenID]; ok {
		t.Fatal("not revoked")
	}
}

func TestAccessTokenCmd_SingularAndPlural(t *testing.T) {
	for _, use := range []string{"access-token", "access-tokens"} {
		f := factoryWith(t, clienttest.New())
		cmd := NewCmd(f)
		cmd.SetArgs([]string{use, "list", "dev"})
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%s: %v", use, err)
		}
	}
}

func TestTranslateAccessTokenErr(t *testing.T) {
	coded := func(status int, code string) error {
		return &client.APIError{StatusCode: status, Code: code, Message: "the API's message"}
	}
	for _, tt := range []struct {
		name string
		err  error
		exit int
		hint string
	}{
		{"limit reached", coded(409, "ACCESS_TOKEN_LIMIT_REACHED"), cli.ExitConflict, "kupe cluster access-tokens list dev"},
		{"expiry invalid", coded(400, "ACCESS_TOKEN_EXPIRY_INVALID"), cli.ExitMisuse, ""},
		{"already collected", coded(410, "ACCESS_TOKEN_ALREADY_COLLECTED"), cli.ExitConflict, "revoke it and create another"},
		{"failed", coded(409, "ACCESS_TOKEN_FAILED"), cli.ExitGeneral, "revoke it and create another"},
		{"creator only", coded(403, "ACCESS_TOKEN_COLLECT_CREATOR_ONLY"), cli.ExitAuth, ""},
		{"admin access required", &client.APIError{StatusCode: 403, Message: "admin access required"}, cli.ExitAuth, "only tenant admins"},
		{"terminating", coded(409, "terminating"), cli.ExitConflict, ""},
		{"tenant suspended (shared)", coded(409, "tenant_suspended"), cli.ExitConflict, "contact support"},
		{"cluster not found", &client.APIError{StatusCode: 404, Message: `cluster "dev" not found`}, cli.ExitNotFound, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := translateAccessTokenErr(tt.err, "dev")
			assertExitCode(t, got, tt.exit)
			if !strings.Contains(got.Error(), "the API's message") && !strings.Contains(got.Error(), "admin access required") && !strings.Contains(got.Error(), "not found") {
				t.Errorf("error text %q lost the API's message", got.Error())
			}
			var ce *cli.Error
			hint := ""
			if errors.As(got, &ce) {
				hint = ce.Hint
			}
			if (tt.hint == "" && hint != "") || !strings.Contains(hint, tt.hint) {
				t.Errorf("hint = %q; want %q", hint, tt.hint)
			}
		})
	}
}

// TestAccessTokenCreate_VerboseNeverLeaksTheToken runs create with -v
// against kupe-api's routes through the real client: the token reaches
// stdout once and nowhere on stderr.
func TestAccessTokenCreate_VerboseNeverLeaksTheToken(t *testing.T) {
	fastAccessTokenWait(t)
	const secret = "eyJhbGciOiJSUzI1NiJ9.live-looking-token.sig"
	kubeconfigYAML := "apiVersion: v1\nkind: Config\nusers:\n- name: u\n  user:\n    token: " + secret + "\n"
	base := "/api/v1/tenants/acme/clusters/dev/access-tokens"
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-1")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":%q,"cluster":"dev","displayName":"ci","access":{"role":"admin"},"status":{"phase":"Pending"}}`, testTokenID)
		case r.Method == http.MethodGet && r.URL.Path == base+"/"+testTokenID:
			polls++
			phase := "Pending"
			if polls > 1 {
				phase = "Ready"
			}
			fmt.Fprintf(w, `{"id":%q,"cluster":"dev","status":{"phase":%q}}`, testTokenID, phase)
		case r.Method == http.MethodPost && r.URL.Path == base+"/"+testTokenID+"/token":
			w.Header().Set("Cache-Control", "no-store")
			body, _ := json.Marshal(map[string]string{"id": testTokenID, "cluster": "dev", "token": secret, "kubeconfig": kubeconfigYAML,
				"endpoint": "https://acme-dev.example.test", "certificateAuthority": "", "expiresAt": "2027-02-20T10:00:00Z"})
			_, _ = w.Write(body)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Cleanup(config.SetURLValidatorForTest(func(string) error { return nil }))
	t.Setenv("KUPE_API_TOKEN", "kupe_test_api_key")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{APIVersion: config.APIVersion, Kind: config.Kind, CurrentContext: "acme",
		Contexts: []config.Context{{Name: "acme", APIURL: srv.URL, Tenant: "acme"}}}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.TestTTY()
	f := cli.NewFactory(io, &cli.GlobalFlags{ConfigPath: cfgPath, Verbose: true})
	out, errOut, err := runAT(context.Background(), f, "create", "dev", "--name", "ci")
	if err != nil {
		t.Fatalf("%v\nstderr:\n%s", err, errOut)
	}
	if !strings.Contains(errOut, "[verbose] POST") {
		t.Fatalf("verbose tracing was not on:\n%s", errOut)
	}
	if strings.Contains(errOut, secret) {
		t.Fatalf("the token reached stderr:\n%s", errOut)
	}
	if out != kubeconfigYAML {
		t.Fatalf("stdout = %q; want the kubeconfig exactly", out)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("the token reached the config file")
	}
}

// failingWriter is a stdout that takes nothing: a full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// F1: a collected token whose output stdout cannot take is revoked.
func TestAccessTokenCreate_StdoutWriteFailureRevokes(t *testing.T) {
	for _, args := range [][]string{{}, {"-o", "json"}} {
		t.Run(strings.Join(append([]string{"text"}, args...), " "), func(t *testing.T) {
			fake := clienttest.New()
			f := factoryWith(t, fake)
			f.IOStreams.Out = failingWriter{}
			cmd := NewCmd(f)
			cmd.SetContext(context.Background())
			cmd.SetArgs(append([]string{"access-tokens", "create", "dev", "--name", "ci"}, args...))
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			err := cmd.Execute()
			assertExitCode(t, err, cli.ExitGeneral)
			if !strings.Contains(err.Error(), "the kubeconfig could not be written to stdout, so the access token was lost; it was revoked") {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Fatal("the token reached the error")
			}
			if calls(fake, "DeleteClusterAccessToken") != 1 {
				t.Fatalf("calls = %v; want a revoke", fake.Calls)
			}
			if _, ok := fake.ClusterAccessTokens[testTokenID]; ok {
				t.Fatal("the lost token is still live")
			}
		})
	}
}

// F1: stdout is a pipe whose reader has gone (`| head -c0`). Go kills a
// process that writes to such an fd 1 with SIGPIPE unless it is ignored, so
// this runs the command in a child process whose real stdout is that pipe
// and checks the child lived to revoke the token.
func TestAccessTokenCreate_ClosedStdoutPipeRevokes(t *testing.T) {
	if os.Getenv("KUPE_TEST_CAT_CLOSED_STDOUT") == "1" {
		fake := clienttest.New()
		f := factoryWith(t, fake)
		f.IOStreams.Out = os.Stdout
		cmd := NewCmd(f)
		cmd.SetContext(context.Background())
		cmd.SetArgs([]string{"access-tokens", "create", "dev", "--name", "ci"})
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		err := cmd.Execute()
		_, live := fake.ClusterAccessTokens[testTokenID]
		fmt.Fprintf(os.Stderr, "child: exit=%d revoked=%v deletes=%d\n", cli.ExitCode(err), !live, calls(fake, "DeleteClusterAccessToken"))
		os.Exit(0)
	}
	if runtime.GOOS == "windows" {
		t.Skip("no SIGPIPE on Windows")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestAccessTokenCreate_ClosedStdoutPipeRevokes$") //#nosec G204 -- this test binary
	child.Env = append(os.Environ(), "KUPE_TEST_CAT_CLOSED_STDOUT=1")
	child.Stdout = w
	var stderr bytes.Buffer
	child.Stderr = &stderr
	runErr := child.Run()
	_ = w.Close()
	if runErr != nil {
		t.Fatalf("child did not exit cleanly (killed by SIGPIPE before revoking?): %v\nstderr:\n%s", runErr, stderr.String())
	}
	if want := "child: exit=1 revoked=true deletes=1"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("child stderr = %q; want %q", stderr.String(), want)
	}
}

// F3: the help names no platform number kupe-api owns: the per-cluster cap
// and the default lifetime are the platform's.
func TestAccessTokenHelp_NoPlatformNumbers(t *testing.T) {
	root := NewCmd(factoryWith(t, clienttest.New()))
	at, _, err := root.Find([]string{"access-tokens"})
	if err != nil {
		t.Fatal(err)
	}
	create, _, err := at.Find([]string{"create"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{at.Long, create.Long, create.Flag("expires-in-days").Usage, create.Flag("expires-in-days").DefValue} {
		for _, banned := range []string{"at most 20", "default 90", "90"} {
			if strings.Contains(text, banned) {
				t.Errorf("help names %q: %s", banned, text)
			}
		}
	}
}
