package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
	"github.com/spf13/cobra"
)

// syncPipe is a small bytes-buffer pipe with goroutine-safe Write/Read so
// the test can scan stderr output line-by-line while runLogin is still
// emitting it. io.Pipe blocks the writer until a reader consumes — that's
// fine for our goroutine pair.
type syncPipe struct {
	mu  sync.Mutex
	buf strings.Builder
	ch  chan struct{}
}

func newSyncPipe() (*syncPipeReader, *syncPipeWriter) {
	p := &syncPipe{ch: make(chan struct{}, 64)}
	return &syncPipeReader{p: p}, &syncPipeWriter{p: p}
}

type syncPipeWriter struct{ p *syncPipe }
type syncPipeReader struct {
	p   *syncPipe
	pos int
}

func (w *syncPipeWriter) Write(b []byte) (int, error) {
	w.p.mu.Lock()
	w.p.buf.Write(b)
	w.p.mu.Unlock()
	select {
	case w.p.ch <- struct{}{}:
	default:
	}
	return len(b), nil
}

func (w *syncPipeWriter) Close() error { return nil }

func (r *syncPipeReader) Read(p []byte) (int, error) {
	for {
		r.p.mu.Lock()
		s := r.p.buf.String()
		if r.pos < len(s) {
			n := copy(p, s[r.pos:])
			r.pos += n
			r.p.mu.Unlock()
			return n, nil
		}
		r.p.mu.Unlock()
		select {
		case <-r.p.ch:
		case <-time.After(10 * time.Second):
			return 0, io.EOF
		}
	}
}

func scanForAuthURL(t *testing.T, r io.Reader, out chan<- string) {
	t.Helper()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if _, err := url.Parse(line); err == nil {
				select {
				case out <- line:
				default:
				}
				return
			}
		}
	}
}

func fireCallback(t *testing.T, authURL, code string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	state := q.Get("state")
	redirect := q.Get("redirect_uri")
	if state == "" || redirect == "" {
		t.Fatalf("auth URL missing state/redirect: %s", authURL)
	}
	cb := redirect + "?code=" + code + "&state=" + state
	resp, err := http.Get(cb) //nolint:gosec
	if err != nil {
		t.Errorf("callback: %v", err)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// testFactory wires a Factory using plaintext storage and a temp-directory
// config — tests never touch the real keyring or the user's home directory.
func testFactory(t *testing.T) (*cli.Factory, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	io, _, _ := cli.Test()
	io.PromptsEnabled = false

	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)

	t.Setenv("KUPE_STORAGE", "plaintext")
	f.Auth = func() (*auth.Manager, error) {
		return auth.NewManager(auth.DefaultCredentialsPath(cfgPath)), nil
	}
	return f, cfgPath
}

// startFakeAPI spins up an httptest server that accepts valid tokens for one
// tenant and 401s otherwise. Returns the URL.
func startFakeAPI(t *testing.T, validTenant, validToken string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-test")
		if got := r.Header.Get("Authorization"); got != "Bearer "+validToken {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, `{"error":"bad token"}`)
			return
		}
		tenant := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
		tenant = strings.TrimSuffix(tenant, "/")
		if tenant != validTenant {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, `{"error":"tenant not found"}`)
			return
		}
		fmt.Fprintln(w, `{"name":"`+tenant+`","displayName":"Acme Corp","plan":"starter"}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// execLogin runs the login RunE path with a real cobra.Command so that
// cmd.Context() works as it does in production.
func execLogin(f *cli.Factory, opts *loginOpts) error {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return runLogin(cmd, f, opts)
}

func TestLoginValidatesAndWritesConfigOnSuccess(t *testing.T) {
	t.Setenv("KUPE_API_TOKEN", "")
	api := startFakeAPI(t, "acme-corp", "kupe_ok")

	f, cfgPath := testFactory(t)

	err := execLogin(f, &loginOpts{
		method: methodToken,
		tenant: "acme-corp",
		token:  "kupe_ok",
		apiURL: api,
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Config must exist and point at the stored token.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Context("acme-corp")
	if ctx == nil || ctx.TokenRef != "plaintext" || ctx.APIURL != api {
		t.Fatalf("context not saved correctly: %+v", ctx)
	}
}

func TestLoginRollsBackOn401(t *testing.T) {
	t.Setenv("KUPE_API_TOKEN", "")
	api := startFakeAPI(t, "acme-corp", "kupe_ok")

	f, cfgPath := testFactory(t)

	err := execLogin(f, &loginOpts{
		method: methodToken,
		tenant: "acme-corp",
		token:  "kupe_wrong", // will 401
		apiURL: api,
	})
	if err == nil {
		t.Fatal("expected login to fail")
	}
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d", code, cli.ExitAuth)
	}

	// Config must NOT be written.
	if cfg, _ := config.Load(cfgPath); cfg.Context("acme-corp") != nil {
		t.Fatal("context was saved despite 401 — rollback failed")
	}

	// Token must NOT be in plaintext storage either.
	mgr, _ := f.Auth()
	if _, err := mgr.GetByRef("acme-corp", "plaintext"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("token leaked into storage after 401: err=%v", err)
	}
}

func TestLoginSurfacesTenantNotFound(t *testing.T) {
	t.Setenv("KUPE_API_TOKEN", "")
	api := startFakeAPI(t, "acme-corp", "kupe_ok")
	f, _ := testFactory(t)

	err := execLogin(f, &loginOpts{
		method: methodToken,
		tenant: "other-tenant",
		token:  "kupe_ok",
		apiURL: api,
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}

func TestLoginRefusesWithKupeAPITokenSet(t *testing.T) {
	t.Setenv("KUPE_API_TOKEN", "kupe_env")
	f, _ := testFactory(t)

	err := execLogin(f, &loginOpts{method: methodToken, tenant: "acme", token: "kupe_x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d", code, cli.ExitAuth)
	}
}

func TestLoginNonInteractiveRequiresTenant(t *testing.T) {
	t.Setenv("KUPE_API_TOKEN", "")
	f, _ := testFactory(t)
	err := execLogin(f, &loginOpts{method: methodToken, token: "kupe_x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
}

func TestLoginRejectsInvalidTenantName(t *testing.T) {
	t.Setenv("KUPE_API_TOKEN", "")
	f, _ := testFactory(t)
	err := execLogin(f, &loginOpts{method: methodToken, tenant: "INVALID", token: "kupe_x"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid tenant name") {
		t.Fatalf("unexpected: %v", err)
	}
}

// TestOIDCLoginEndToEnd drives runLogin through the OIDC path with a fake
// Authentik (token endpoint) and a fake kupe-api. The test's "browser" is
// a goroutine that fetches the auth URL and posts a synthesised callback
// straight at the CLI's localhost listener.
func TestOIDCLoginEndToEnd(t *testing.T) {
	t.Cleanup(auth.SetBrowserOpenerForTest(func(string) error { return nil }))
	t.Setenv("KUPE_API_TOKEN", "")
	const wantCode = "test-auth-code"

	authentikMux := http.NewServeMux()
	authentik := httptest.NewServer(authentikMux)
	t.Cleanup(authentik.Close)
	authentikMux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/"
        }`, authentik.URL, authentik.URL, authentik.URL)
	})
	authentikMux.HandleFunc("/application/o/token/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("code"); got != wantCode {
			t.Errorf("code=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		// Build a minimal id_token with an email claim so login persists user.
		hdr := "eyJhbGciOiJub25lIn0"         // {"alg":"none"}
		body := "eyJlbWFpbCI6InVAYS5jb20ifQ" // {"email":"u@a.com"}
		fmt.Fprintf(w, `{"access_token":"oidc-access","refresh_token":"oidc-refresh","id_token":"%s.%s.sig","expires_in":3600}`, hdr, body)
	})

	api := startFakeAPI(t, "acme-corp", "oidc-access")

	f, cfgPath := testFactory(t)

	// Spawn a fake browser that watches the CLI's stderr for the auth URL
	// and fires a synthesised callback. We swap in an io.Pipe so the
	// goroutine can read the URL line as soon as runLogin emits it.
	originalErr := f.IOStreams.ErrOut
	pr, pw := newSyncPipe()
	f.IOStreams.ErrOut = pw
	t.Cleanup(func() {
		f.IOStreams.ErrOut = originalErr
		_ = pw.Close()
	})

	authURLCh := make(chan string, 1)
	go scanForAuthURL(t, pr, authURLCh)
	go func() {
		select {
		case authURL := <-authURLCh:
			fireCallback(t, authURL, wantCode)
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for auth URL")
		}
	}()

	err := execLogin(f, &loginOpts{
		method:       methodOIDC,
		tenant:       "acme-corp",
		apiURL:       api,
		oidcBaseURL:  authentik.URL,
		oidcClientID: "kupe-cli",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Context("acme-corp")
	if ctx == nil {
		t.Fatal("context not saved")
	}
	if ctx.AuthMethod != config.AuthMethodOIDC {
		t.Errorf("AuthMethod=%q; want oidc", ctx.AuthMethod)
	}
	if ctx.User != "u@a.com" {
		t.Errorf("User=%q; want u@a.com (from id_token)", ctx.User)
	}
	if !strings.HasPrefix(ctx.OIDCBaseURL, authentik.URL) {
		t.Errorf("OIDCBaseURL=%q; want it persisted to override default", ctx.OIDCBaseURL)
	}

	mgr, _ := f.Auth()
	stored, err := mgr.GetByRef("acme-corp", "plaintext")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.IsOIDCBlob(stored) {
		t.Fatalf("stored credential is not an OIDC blob: %q", stored)
	}
	ts, err := auth.UnmarshalOIDC(stored)
	if err != nil {
		t.Fatal(err)
	}
	if ts.AccessToken != "oidc-access" || ts.RefreshToken != "oidc-refresh" {
		t.Fatalf("stored token set wrong: %+v", ts)
	}
}

// guard: cli.ExitCode maps *client.APIError directly for classes the client
// emits. This mirrors the logic tested above via login paths.
func TestExitCodeFromClientError(t *testing.T) {
	tests := []struct {
		status int
		want   int
	}{
		{http.StatusUnauthorized, cli.ExitAuth},
		{http.StatusForbidden, cli.ExitAuth},
		{http.StatusNotFound, cli.ExitNotFound},
		{http.StatusConflict, cli.ExitConflict},
		{http.StatusPreconditionFailed, cli.ExitConflict},
		{http.StatusTooManyRequests, cli.ExitRateLimited},
		{http.StatusServiceUnavailable, cli.ExitUnavailable},
		{http.StatusBadRequest, cli.ExitMisuse},
	}
	for _, tt := range tests {
		err := &client.APIError{StatusCode: tt.status}
		if got := cli.ExitCode(err); got != tt.want {
			t.Errorf("%d → exit %d; want %d", tt.status, got, tt.want)
		}
	}
}
