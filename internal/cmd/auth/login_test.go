package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
	"github.com/spf13/cobra"
)

// testFactory wires a Factory using plaintext storage and a temp-directory
// config — tests never touch the real keyring or the user's home directory.
// Also relaxes the kupe.cloud URL validator so httptest.NewServer's
// 127.0.0.1 URLs pass; the strict validator is restored at test end.
func testFactory(t *testing.T) (*cli.Factory, string) {
	t.Helper()
	t.Cleanup(config.SetURLValidatorForTest(func(string) error { return nil }))
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

// startFakeAuthentik serves the OIDC discovery, device and token endpoints
// for a device flow that is approved on the first poll. The token response
// carries access token "oidc-access" and an id_token with email u@a.com.
func startFakeAuthentik(t *testing.T) string {
	t.Helper()
	authentikMux := http.NewServeMux()
	authentik := httptest.NewServer(authentikMux)
	t.Cleanup(authentik.Close)

	authentikMux.HandleFunc("/application/o/kupe-cli/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "issuer":"%s/application/o/kupe-cli/",
            "authorization_endpoint":"%s/application/o/authorize/",
            "token_endpoint":"%s/application/o/token/",
            "device_authorization_endpoint":"%s/application/o/device/"
        }`, authentik.URL, authentik.URL, authentik.URL, authentik.URL)
	})
	authentikMux.HandleFunc("/application/o/device/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "device_code":"dev-123",
            "user_code":"WDJB-MJHT",
            "verification_uri":"%s/device",
            "expires_in":600,
            "interval":1
        }`, authentik.URL)
	})
	authentikMux.HandleFunc("/application/o/token/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("grant_type=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		// Minimal id_token with an email claim so login persists user.
		hdr := "eyJhbGciOiJub25lIn0"         // {"alg":"none"}
		body := "eyJlbWFpbCI6InVAYS5jb20ifQ" // {"email":"u@a.com"}
		fmt.Fprintf(w, `{"access_token":"oidc-access","refresh_token":"oidc-refresh","id_token":"%s.%s.sig","expires_in":3600}`, hdr, body)
	})
	return authentik.URL
}

// TestOIDCLoginEndToEnd drives runLogin through the OIDC path against a
// fake Authentik that runs a complete RFC 8628 device flow and a fake
// kupe-api that validates the resulting access token. Verifies the
// stored OIDCTokenSet, the persisted Context fields, and the email
// extracted from the id_token.
func TestOIDCLoginEndToEnd(t *testing.T) {
	t.Cleanup(auth.SetBrowserOpenerForTest(func(string) error { return nil }))
	t.Setenv("KUPE_API_TOKEN", "")

	authentikURL := startFakeAuthentik(t)

	api := startFakeAPI(t, "acme-corp", "oidc-access")

	f, cfgPath := testFactory(t)

	err := execLogin(f, &loginOpts{
		method:       methodOIDC,
		tenant:       "acme-corp",
		apiURL:       api,
		oidcBaseURL:  authentikURL,
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
	if !strings.HasPrefix(ctx.OIDCBaseURL, authentikURL) {
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

// TestLoginSwitchesCurrentContext pins issue #69: login makes the logged-in
// context current by default and says so, naming the previous context only
// when there was a different one. Covers the token path end to end; the OIDC
// path calls the same helpers.
func TestLoginSwitchesCurrentContext(t *testing.T) {
	tests := []struct {
		name        string
		existing    []string // contexts already in the config
		current     string   // currentContext before login
		noSwitch    bool
		wantCurrent string
		wantStderr  string // "" means no "Switched" line at all
	}{
		{
			name:        "first login switches with no previous",
			wantCurrent: "acme-corp",
			wantStderr:  "Switched current context to acme-corp.\n",
		},
		{
			name:        "new tenant switches away from another context",
			existing:    []string{"aware"},
			current:     "aware",
			wantCurrent: "acme-corp",
			wantStderr:  "Switched current context to acme-corp (was aware).\n",
		},
		{
			name:        "re-login to the current context prints nothing",
			existing:    []string{"aware", "acme-corp"},
			current:     "acme-corp",
			wantCurrent: "acme-corp",
		},
		{
			name:        "no-switch keeps the current context",
			existing:    []string{"aware"},
			current:     "aware",
			noSwitch:    true,
			wantCurrent: "aware",
		},
		{
			name:        "no-switch replaces a current context that no longer exists",
			existing:    []string{"aware"},
			current:     "ghost",
			noSwitch:    true,
			wantCurrent: "acme-corp",
			wantStderr:  "Switched current context to acme-corp (was ghost).\n",
		},
		{
			name:        "no-switch still sets a context when none is current",
			existing:    []string{"aware"},
			noSwitch:    true,
			wantCurrent: "acme-corp",
			wantStderr:  "Switched current context to acme-corp.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUPE_API_TOKEN", "")
			api := startFakeAPI(t, "acme-corp", "kupe_ok")
			f, cfgPath := testFactory(t)
			io, _, errOut := cli.Test()
			f.IOStreams = io

			seed := &config.Config{CurrentContext: tt.current}
			for _, name := range tt.existing {
				seed.SetContext(config.Context{Name: name, Tenant: name})
			}
			if err := seed.Save(cfgPath); err != nil {
				t.Fatal(err)
			}

			if err := execLogin(f, &loginOpts{
				method:   methodToken,
				tenant:   "acme-corp",
				token:    "kupe_ok",
				apiURL:   api,
				noSwitch: tt.noSwitch,
			}); err != nil {
				t.Fatalf("login: %v", err)
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CurrentContext != tt.wantCurrent {
				t.Errorf("currentContext=%q; want %q", cfg.CurrentContext, tt.wantCurrent)
			}
			got := ""
			for line := range strings.Lines(errOut.String()) {
				if strings.HasPrefix(line, "Switched current context") {
					got += line
				}
			}
			if got != tt.wantStderr {
				t.Errorf("switch line=%q; want %q (stderr: %q)", got, tt.wantStderr, errOut.String())
			}
		})
	}
}

// TestLoginSetDefaultIsHiddenNoOp keeps old scripts that pass --set-default
// working now that switching is the default.
func TestLoginSetDefaultIsHiddenNoOp(t *testing.T) {
	cmd := newLoginCmd(nil)
	fl := cmd.Flags().Lookup("set-default")
	if fl == nil {
		t.Fatal("--set-default removed; scripts passing it would fail")
	}
	if !fl.Hidden {
		t.Error("--set-default should be hidden")
	}
	if cmd.Flags().Lookup("no-switch") == nil {
		t.Error("--no-switch flag missing")
	}
}

// TestOIDCLoginSwitchesCurrentContext pins issue #69 on the OIDC path, the
// one a tenant hits with a plain `kupe auth login`: with another context
// current, login switches to the new one and names the old one.
func TestOIDCLoginSwitchesCurrentContext(t *testing.T) {
	for _, noSwitch := range []bool{false, true} {
		t.Run(fmt.Sprintf("noSwitch=%v", noSwitch), func(t *testing.T) {
			t.Cleanup(auth.SetBrowserOpenerForTest(func(string) error { return nil }))
			t.Setenv("KUPE_API_TOKEN", "")
			authentikURL := startFakeAuthentik(t)
			api := startFakeAPI(t, "acme-corp", "oidc-access")
			f, cfgPath := testFactory(t)
			io, _, errOut := cli.Test()
			f.IOStreams = io

			seed := &config.Config{CurrentContext: "aware"}
			seed.SetContext(config.Context{Name: "aware", Tenant: "aware"})
			if err := seed.Save(cfgPath); err != nil {
				t.Fatal(err)
			}

			if err := execLogin(f, &loginOpts{
				method:       methodOIDC,
				tenant:       "acme-corp",
				apiURL:       api,
				oidcBaseURL:  authentikURL,
				oidcClientID: "kupe-cli",
				noSwitch:     noSwitch,
			}); err != nil {
				t.Fatalf("login: %v", err)
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			wantCurrent, wantLine := "acme-corp", "Switched current context to acme-corp (was aware).\n"
			if noSwitch {
				wantCurrent, wantLine = "aware", ""
			}
			if cfg.CurrentContext != wantCurrent {
				t.Errorf("currentContext=%q; want %q", cfg.CurrentContext, wantCurrent)
			}
			if got := strings.Contains(errOut.String(), "Switched current context"); got != (wantLine != "") ||
				(wantLine != "" && !strings.Contains(errOut.String(), wantLine)) {
				t.Errorf("stderr=%q; want switch line %q", errOut.String(), wantLine)
			}
		})
	}
}
