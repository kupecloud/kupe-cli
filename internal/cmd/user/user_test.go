package user

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// factoryWith wires a Factory over a temp config whose "acme" context is
// authenticated with authMethod and has a stored plaintext credential, so
// the post-delete logout path exercises the real auth manager. Returns the
// factory and the config path for re-loading assertions.
func factoryWith(t *testing.T, fake *clienttest.FakeSignup, authMethod, user string) (*cli.Factory, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("KUPE_STORAGE", "plaintext")
	t.Setenv("KUPE_API_TOKEN", "")

	mgr := auth.NewManager(auth.DefaultCredentialsPath(cfgPath))
	ref, err := mgr.Set("acme", "stored-token")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: ref, AuthMethod: authMethod, User: user},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	f := cli.NewFactory(io, &cli.GlobalFlags{ConfigPath: cfgPath})
	f.Auth = func() (*auth.Manager, error) { return mgr, nil }
	f.SignupClient = func() (client.SignupInterface, error) { return fake, nil }
	return f, cfgPath
}

func run(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func stdout(f *cli.Factory) string { return f.IOStreams.Out.(interface{ String() string }).String() }

func TestDeleteRefusesAPIKeyContext(t *testing.T) {
	fake := clienttest.NewSignup()
	f, _ := factoryWith(t, fake, config.AuthMethodAPIKey, "")
	err := run(NewCmd(f), "delete", "--confirm", "billy@acme.com")
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitAuth, err)
	}
	if !strings.Contains(err.Error(), "OIDC") {
		t.Fatalf("message should explain OIDC login is required: %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("must not call signup with an API key: %v", fake.Calls)
	}
}

func TestDeleteRequiresConfirm(t *testing.T) {
	fake := clienttest.NewSignup()
	f, _ := factoryWith(t, fake, config.AuthMethodOIDC, "billy@acme.com")
	err := run(NewCmd(f), "delete")
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitMisuse, err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("must not call signup without --confirm: %v", fake.Calls)
	}
}

func TestDeleteConfirmMustMatchLoggedInUser(t *testing.T) {
	fake := clienttest.NewSignup()
	f, _ := factoryWith(t, fake, config.AuthMethodOIDC, "billy@acme.com")
	err := run(NewCmd(f), "delete", "--confirm", "someone-else@acme.com")
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitMisuse, err)
	}
	if !strings.Contains(err.Error(), "billy@acme.com") {
		t.Fatalf("message should name the logged-in user: %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("must not call signup on a mismatched --confirm: %v", fake.Calls)
	}
}

func TestDeleteSuccessLogsOutAndPrints(t *testing.T) {
	fake := clienttest.NewSignup()
	f, cfgPath := factoryWith(t, fake, config.AuthMethodOIDC, "billy@acme.com")
	if err := run(NewCmd(f), "delete", "--confirm", "Billy@acme.com"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := strings.TrimSpace(stdout(f)); got != "user/Billy@acme.com deleted" {
		t.Fatalf("stdout = %q", got)
	}
	if len(fake.DeleteUserRequests) != 1 || fake.DeleteUserRequests[0].Confirm != "Billy@acme.com" {
		t.Fatalf("requests = %+v", fake.DeleteUserRequests)
	}
	// Credentials gone, tokenRef cleared — the `auth logout` equivalent.
	mgr, _ := f.Auth()
	if _, err := mgr.GetByRef("acme", "plaintext"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("stored credential should be removed; GetByRef err = %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if ctx := cfg.Context("acme"); ctx == nil || ctx.TokenRef != "" {
		t.Fatalf("context tokenRef should be cleared: %+v", ctx)
	}
}

func TestDeleteWithDirectOIDCTokenSkipsLocalLogout(t *testing.T) {
	fake := clienttest.NewSignup()
	f, cfgPath := factoryWith(t, fake, config.AuthMethodAPIKey, "")
	f.Flags.Token = "eyJhbGciOi.eyJzdWIiOi.sig" // OIDC JWT via --token: no local email, no stored cred to drop
	if err := run(NewCmd(f), "delete", "--confirm", "billy@acme.com"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %v", fake.Calls)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if ctx := cfg.Context("acme"); ctx == nil || ctx.TokenRef != "plaintext" {
		t.Fatalf("a direct-token run must not touch the stored context credential: %+v", ctx)
	}
}

func TestDeleteErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantExit int
		wantMsg  string
		wantHint string
	}{
		{"409 tenant_memberships", &client.TenantMembershipsError{
			Err:     &client.APIError{StatusCode: 409, Code: "tenant_memberships", Message: "leave first"},
			Tenants: []string{"acme", "beta"},
		}, cli.ExitConflict, "2 tenant(s): acme, beta", "kupe tenant delete"},
		{"409 without list", &client.TenantMembershipsError{
			Err: &client.APIError{StatusCode: 409, Message: "you belong to tenant acme"},
		}, cli.ExitConflict, "still belongs to one or more tenants", "kupe member remove"},
		{"403", &client.APIError{StatusCode: 403, Message: "stale auth_time"}, cli.ExitAuth, "refused to delete", "kupe auth login"},
		{"429", &client.APIError{StatusCode: 429, Message: "slow down"}, cli.ExitRateLimited, "rate limited", "retry"},
		{"400", &client.APIError{StatusCode: 400, Message: "confirm mismatch"}, cli.ExitMisuse, "rejected the confirmation", "whoami"},
		{"503", &client.APIError{StatusCode: 503, Message: "down"}, cli.ExitUnavailable, "down", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.NewSignup()
			fake.DeleteUserErr = tt.err
			f, cfgPath := factoryWith(t, fake, config.AuthMethodOIDC, "billy@acme.com")
			err := run(NewCmd(f), "delete", "--confirm", "billy@acme.com")
			if err == nil {
				t.Fatal("want error")
			}
			if code := cli.ExitCode(err); code != tt.wantExit {
				t.Fatalf("exit = %d; want %d (%v)", code, tt.wantExit, err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("message %q missing %q", err.Error(), tt.wantMsg)
			}
			var ce *cli.Error
			if tt.wantHint != "" && (!errors.As(err, &ce) || !strings.Contains(ce.Hint, tt.wantHint)) {
				t.Fatalf("hint missing %q: %v", tt.wantHint, err)
			}
			if stdout(f) != "" {
				t.Fatalf("no stdout on failure, got %q", stdout(f))
			}
			// Credentials must survive a failed delete.
			cfg, _ := config.Load(cfgPath)
			if ctx := cfg.Context("acme"); ctx == nil || ctx.TokenRef == "" {
				t.Fatalf("failed delete must not log out: %+v", ctx)
			}
		})
	}
}
