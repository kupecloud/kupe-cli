package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

const fakeSecret = "kupecr_fake0123456789abcdef"

func TestTokenCreate_StdoutOnlyHasSecret(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	out, errOut, err := run(f, "token", "create", "--name", "ci", "--access", "pull")
	if err != nil {
		t.Fatal(err)
	}
	if out != fakeSecret+"\n" {
		t.Fatalf("stdout should be the secret only, got %q", out)
	}
	if errOut != "" {
		t.Fatalf("stderr should be empty on non-TTY, got %q", errOut)
	}
}

func TestTokenCreate_JSONHasEveryField(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	out, _, err := run(f, "token", "create", "--name", "ci", "--access", "push", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for key, want := range map[string]any{
		"id": "rt-0123456789abcdef", "name": "ci", "access": "push", "username": "kupe+acme+tok-0123456789abcdef",
		"registry": "registry.kupe.cloud", "secret": fakeSecret, "createdBy": "tester@acme.com",
		"createdAt": "2026-10-22T10:00:00Z", "expiresAt": "2027-01-20T10:00:00Z", "phase": "Ready",
	} {
		if got[key] != want {
			t.Errorf("%s = %v; want %v", key, got[key], want)
		}
	}
}

func TestTokenCreate_TTYHintQuotesTheUsername(t *testing.T) {
	fake := seeded()
	io, _, _ := cli.TestTTY()
	f := factoryWithStreams(t, fake, io)
	out, errOut, err := run(f, "token", "create", "--name", "ci", "--access", "pull")
	if err != nil {
		t.Fatal(err)
	}
	want := `printf '%s' "$TOKEN" | docker login registry.kupe.cloud --username 'kupe+acme+tok-0123456789abcdef' --password-stdin`
	if !strings.Contains(errOut, want) {
		t.Fatalf("stderr missing %q:\n%s", want, errOut)
	}
	if strings.Contains(errOut, fakeSecret) {
		t.Fatal("the secret reached stderr")
	}
	if out != fakeSecret+"\n" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestTokenCreate_ExpiresInDays(t *testing.T) {
	fake := seeded()
	if _, _, err := run(factoryWith(t, fake), "token", "create", "--name", "ci", "--access", "pull", "--expires-in-days", "30"); err != nil {
		t.Fatal(err)
	}
	if d := fake.RegistryTokenCreates[0].ExpiresInDays; d == nil || *d != 30 {
		t.Fatalf("expiresInDays = %v; want 30", d)
	}
	fake = seeded()
	if _, _, err := run(factoryWith(t, fake), "token", "create", "--name", "ci", "--access", "pull"); err != nil {
		t.Fatal(err)
	}
	if d := fake.RegistryTokenCreates[0].ExpiresInDays; d != nil {
		t.Fatalf("expiresInDays = %v; want omitted", *d)
	}
	body, _ := json.Marshal(fake.RegistryTokenCreates[0])
	if strings.Contains(string(body), "expiresInDays") {
		t.Fatalf("body = %s", body)
	}
	for _, days := range []string{"0", "366", "-1"} {
		fake := seeded()
		_, _, err := run(factoryWith(t, fake), "token", "create", "--name", "ci", "--access", "pull", "--expires-in-days", days)
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "CreateRegistryToken")
	}
	for _, args := range [][]string{
		{"--access", "pull"}, {"--name", "ci"}, {"--name", "ci", "--access", "admin"}, {"--name", "  ", "--access", "pull"},
		{"--name", "ci", "--access", "pull", "-o", "yaml"},
	} {
		_, _, err := run(factoryWith(t, seeded()), append([]string{"token", "create"}, args...)...)
		assertExit(t, err, cli.ExitMisuse)
	}
}

func TestTokenCreate_NotIssuedIsRevoked(t *testing.T) {
	fake := seeded()
	fake.CreatedRegistryTokenPhase = client.PhasePending
	f := factoryWith(t, fake)
	out, _, err := run(f, "token", "create", "--name", "ci", "--access", "pull", "--wait-timeout", "30ms")
	assertExit(t, err, cli.ExitUnavailable)
	if out != "" {
		t.Fatalf("a secret was printed for a token that does not work: %q", out)
	}
	if _, ok := fake.RegistryTokens["rt-0123456789abcdef"]; ok {
		t.Fatal("the token was not revoked")
	}
}

func TestTokenList_Kinds(t *testing.T) {
	for _, tt := range []struct{ flag, want string }{{"all", ""}, {"token", "token"}, {"session", "session"}} {
		fake := seeded()
		if _, _, err := run(factoryWith(t, fake), "token", "list", "--kind", tt.flag); err != nil {
			t.Fatal(err)
		}
		if !hasCall(fake, "ListRegistryTokens:registry:"+tt.want) {
			t.Errorf("--kind %s: calls = %v", tt.flag, fake.Calls)
		}
	}
	_, _, err := run(factoryWith(t, seeded()), "token", "list", "--kind", "robot")
	assertExit(t, err, cli.ExitMisuse)
}

func TestTokenRevoke(t *testing.T) {
	fake := seeded()
	out, _, err := run(factoryWith(t, fake), "token", "revoke", "rt-00000000000000aa", "--yes")
	if err != nil || out != "token/rt-00000000000000aa revoked\n" || len(fake.RegistryTokens) != 0 {
		t.Fatalf("out %q, err %v", out, err)
	}
	_, _, err = run(factoryWith(t, seeded()), "token", "revoke", "rt-00000000000000aa")
	assertExit(t, err, cli.ExitMisuse)
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"kupe+acme+tok-0123456789abcdef": `'kupe+acme+tok-0123456789abcdef'`,
		"a$b":                            `'a$b'`,
		"a'b":                            `'a'\''b'`,
		"":                               `''`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s; want %s", in, got, want)
		}
	}
	if shellArg("registry.kupe.cloud") != "registry.kupe.cloud" || shellArg("a b") != `'a b'` {
		t.Error("shellArg quotes wrongly")
	}
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell")
	}
	for in := range cases {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(in)).Output() // #nosec G204 -- the quoting under test
		if err != nil {
			t.Fatalf("sh: %v", err)
		}
		if string(out) != in {
			t.Errorf("round trip of %q gave %q", in, out)
		}
	}
}

func TestLogin_WaitsForUsername(t *testing.T) {
	fake := seeded()
	fake.CreatedRegistryTokenPhase = client.PhasePending
	issued := &client.RegistryToken{ID: "rt-0123456789abcdef", Kind: "session", Username: "kupe+acme+cli-issued",
		Status: &client.RegistryTokenStatus{Phase: client.PhaseReady}}
	fake.GetRegistryTokenSeq["rt-0123456789abcdef"] = []*client.RegistryToken{
		{ID: "rt-0123456789abcdef", Status: &client.RegistryTokenStatus{Phase: client.PhasePending}},
		{ID: "rt-0123456789abcdef", Status: &client.RegistryTokenStatus{Phase: client.PhaseReady}}, // no username yet
		issued,
	}
	dir := t.TempDir()
	f := factoryWith(t, fake)
	out, errOut, err := run(f, "login", "--docker-config", dir)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path) // #nosec G304 -- the test's own file
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Auths map[string]struct{ Auth string } `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(cfg.Auths["registry.kupe.cloud"].Auth)
	if string(decoded) != "kupe+acme+cli-issued:"+fakeSecret {
		t.Fatalf("stored %q; want the issued username, not the predicted one", decoded)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
	for _, want := range []string{
		"Your Docker credential was saved unencrypted in " + path,
		"Logged in to registry.kupe.cloud as 'kupe+acme+cli-issued' until ",
		"(12 hours). Admins can push and pull; read-only members can pull.",
		`Run "kupe registry login" again after it expires: Docker reports "unauthorized" once it has.`, //nolint:misspell // Docker's text
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

func TestLogin_NoStoreJSONWritesNothing(t *testing.T) {
	fake := seeded()
	dir := t.TempDir()
	f := factoryWith(t, fake)
	out, _, err := run(f, "login", "--no-store", "-o", "json", "--docker-config", dir)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got["server"] != "registry.kupe.cloud" || got["username"] != "kupe+acme+cli-0123456789abcdef" || got["password"] != fakeSecret || got["expiresAt"] == "" {
		t.Fatalf("json = %v", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("--no-store wrote the Docker config")
	}
	for _, args := range [][]string{{"--no-store"}, {"-o", "json"}, {"-o", "yaml", "--no-store"}} {
		fake := seeded()
		_, _, err := run(factoryWith(t, fake), append([]string{"login"}, args...)...)
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "CreateRegistrySession")
	}
}

func TestLogin_APIKeyRefused(t *testing.T) {
	fake := seeded()
	fake.CreateRegistrySessionErr = &client.APIError{StatusCode: 403, Code: client.RegistryCodeSessionRequiresUser,
		Message: "login sessions are for people signed in with kupe; use an access token for automation"}
	_, _, err := run(factoryWith(t, fake), "login", "--docker-config", t.TempDir())
	assertExit(t, err, cli.ExitAuth)
	if !strings.Contains(hint(err), "sign in with:  kupe auth login") || !strings.Contains(hint(err), "kupe registry token create") {
		t.Fatalf("hint = %q", hint(err))
	}
}

func TestLogin_TimeoutRevokes(t *testing.T) {
	old := issueTimeout
	issueTimeout = 30 * time.Millisecond
	t.Cleanup(func() { issueTimeout = old })
	fake := seeded()
	fake.CreatedRegistryTokenPhase = client.PhasePending
	dir := t.TempDir()
	_, _, err := run(factoryWith(t, fake), "login", "--docker-config", dir)
	assertExit(t, err, cli.ExitUnavailable)
	if !strings.Contains(err.Error(), "the registry did not issue the login session in time; it was revoked") || hint(err) != "try again:  kupe registry login" {
		t.Fatalf("err = %v, hint %q", err, hint(err))
	}
	if !hasCall(fake, "DeleteRegistryToken:registry:rt-0123456789abcdef") {
		t.Fatalf("calls = %v", fake.Calls)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("a credential was stored")
	}
}

// cancelOnPoll is the fake with Ctrl+C pressed during the wait: the first
// poll of the token cancels the command's context.
type cancelOnPoll struct {
	*clienttest.Fake
	cancel context.CancelFunc
}

func (c cancelOnPoll) GetRegistryToken(ctx context.Context, registry, id string) (*client.RegistryToken, error) {
	c.cancel()
	return c.Fake.GetRegistryToken(ctx, registry, id)
}

// F-1 (RB): Ctrl+C during the wait still revokes the token or session,
// because the revoke runs on a context the cancellation does not reach.
func TestCtrlCStillRevokes(t *testing.T) {
	for _, tt := range []struct {
		what string
		args []string
	}{
		{"login session", []string{"login", "--docker-config", ""}},
		{"access token", []string{"token", "create", "--name", "ci", "--access", "pull"}},
	} {
		t.Run(tt.what, func(t *testing.T) {
			fake := seeded()
			fake.CreatedRegistryTokenPhase = client.PhasePending
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := factoryWith(t, fake)
			f.Client = func() (client.Interface, error) { return cancelOnPoll{Fake: fake, cancel: cancel}, nil }
			args := slices.Clone(tt.args)
			if args[0] == "login" {
				args[2] = t.TempDir()
			}
			out, _, err := runCtx(ctx, f, args...)
			assertExit(t, err, cli.ExitInterrupted)
			if want := "stopped waiting for the " + tt.what + "; it was revoked"; !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v; want %q", err, want)
			}
			if _, ok := fake.RegistryTokens["rt-0123456789abcdef"]; ok {
				t.Fatal("the cancelled " + tt.what + " was not revoked")
			}
			if out != "" {
				t.Fatalf("stdout = %q", out)
			}
		})
	}
}

// F-1 (RG): a credential that cannot be stored revokes the session it was
// for.
func TestLogin_StoreFailureRevokes(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { // #nosec G302 -- read-only on purpose, so the store fails
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // #nosec G302 -- restored so the temp dir can be removed
	fake := seeded()
	_, errOut, err := run(factoryWith(t, fake), "login", "--docker-config", dir)
	assertExit(t, err, cli.ExitGeneral)
	if !hasCall(fake, "DeleteRegistryToken:registry:rt-0123456789abcdef") {
		t.Fatalf("calls = %v", fake.Calls)
	}
	if _, ok := fake.RegistryTokens["rt-0123456789abcdef"]; ok {
		t.Fatal("the session whose credential was not stored is still live")
	}
	if strings.Contains(errOut, "could not be revoked") || strings.Contains(errOut, "Logged in") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestLogin_HelperChecksComeFirst(t *testing.T) {
	t.Run("an invalid helper name is misuse", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "config.json"), `{"credsStore":"../evil"}`)
		fake := seeded()
		_, _, err := run(factoryWith(t, fake), "login", "--docker-config", dir)
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "CreateRegistrySession")
	})
	t.Run("a missing helper is named", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "config.json"), `{"credHelpers":{"registry.kupe.cloud":"nothere"}}`)
		fake := seeded()
		_, _, err := run(factoryWith(t, fake), "login", "--docker-config", dir)
		assertExit(t, err, cli.ExitGeneral)
		if !strings.Contains(err.Error(), "docker-credential-nothere") {
			t.Fatalf("err = %v", err)
		}
		assertNoCall(t, fake, "CreateRegistrySession")
	})
	// F-2: the helper checked is the one for the registry's own host, not
	// the default host's.
	t.Run("the registry's host chooses the helper", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		for _, tt := range []struct {
			helper string
			exit   int
		}{{"nothere", cli.ExitGeneral}, {"../evil", cli.ExitMisuse}} {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "config.json"), `{"credHelpers":{"cr.other.example":"`+tt.helper+`"}}`)
			fake := seeded()
			fake.RegistriesByName[client.RegistryName].Status.Endpoint = "cr.other.example/acme"
			_, _, err := run(factoryWith(t, fake), "login", "--docker-config", dir)
			assertExit(t, err, tt.exit)
			if !strings.Contains(err.Error(), "credHelpers") {
				t.Errorf("%s: err = %v; want the config key that chose it", tt.helper, err)
			}
			assertNoCall(t, fake, "CreateRegistrySession")
		}
	})
}

func TestLogout_ErasesAndRevokesOwnSessions(t *testing.T) {
	fake := seeded()
	fake.RegistryTokens["rt-mine"] = &client.RegistryToken{ID: "rt-mine", Kind: "session", CreatedBy: "Tester@acme.com"}
	fake.RegistryTokens["rt-theirs"] = &client.RegistryToken{ID: "rt-theirs", Kind: "session", CreatedBy: "other@acme.com"}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), `{"auths":{"registry.kupe.cloud":{"auth":"eA=="},"ghcr.io":{"auth":"eQ=="}}}`)
	_, errOut, err := run(factoryWith(t, fake), "logout", "--docker-config", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.RegistryTokens["rt-mine"]; ok {
		t.Fatal("own session not revoked")
	}
	if _, ok := fake.RegistryTokens["rt-theirs"]; !ok {
		t.Fatal("another member's session was revoked")
	}
	if _, ok := fake.RegistryTokens["rt-00000000000000aa"]; !ok {
		t.Fatal("an access token was revoked")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.json")) // #nosec G304 -- the test's own file
	if strings.Contains(string(data), "registry.kupe.cloud") || !strings.Contains(string(data), "ghcr.io") {
		t.Fatalf("config = %s", data)
	}
	if !strings.Contains(errOut, "Removed the Docker credential for registry.kupe.cloud.") || !strings.Contains(errOut, "Revoked 1 login session.") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestLogout_RevokeFailureIsAWarning(t *testing.T) {
	fake := seeded()
	fake.RegistryTokens["rt-mine"] = &client.RegistryToken{ID: "rt-mine", Kind: "session", CreatedBy: testUser}
	fake.DeleteRegistryTokenErr = &client.APIError{StatusCode: 503, Message: "busy"}
	_, errOut, err := run(factoryWith(t, fake), "logout", "--docker-config", t.TempDir())
	if err != nil || !strings.Contains(errOut, "Warning: could not revoke login session rt-mine") {
		t.Fatalf("err = %v, stderr %q", err, errOut)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
