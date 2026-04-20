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

	err := execLogin(f, &loginOpts{tenant: "acme", token: "kupe_x"})
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
	err := execLogin(f, &loginOpts{token: "kupe_x"})
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
	err := execLogin(f, &loginOpts{tenant: "INVALID", token: "kupe_x"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid tenant name") {
		t.Fatalf("unexpected: %v", err)
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
