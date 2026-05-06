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

// TestOIDCLoginEndToEnd drives runLogin through the OIDC path against a
// fake Authentik that runs a complete RFC 8628 device flow and a fake
// kupe-api that validates the resulting access token. Verifies the
// stored OIDCTokenSet, the persisted Context fields, and the email
// extracted from the id_token.
func TestOIDCLoginEndToEnd(t *testing.T) {
	t.Cleanup(auth.SetBrowserOpenerForTest(func(string) error { return nil }))
	t.Setenv("KUPE_API_TOKEN", "")

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

	api := startFakeAPI(t, "acme-corp", "oidc-access")

	f, cfgPath := testFactory(t)

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
