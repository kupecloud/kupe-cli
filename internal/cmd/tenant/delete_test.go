package tenant

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// oidcFactory is factoryWith for an OIDC-authenticated "acme" context — the
// only kind of context tenant delete accepts.
func oidcFactory(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext", AuthMethod: config.AuthMethodOIDC, User: "owner@acme.com"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	f := cli.NewFactory(io, &cli.GlobalFlags{ConfigPath: cfgPath})
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

func stdout(f *cli.Factory) string { return f.IOStreams.Out.(interface{ String() string }).String() }
func stderr(f *cli.Factory) string { return f.IOStreams.ErrOut.(interface{ String() string }).String() }

func hasCall(fake *clienttest.Fake, prefix string) bool {
	for _, c := range fake.Calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestDeleteRefusesAPIKeyContext(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake) // authMethod empty == apikey
	err := run(NewCmd(f), "delete", "--confirm", "acme")
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitAuth, err)
	}
	if !strings.Contains(err.Error(), "OIDC") {
		t.Fatalf("message should explain OIDC login is required: %v", err)
	}
	if hasCall(fake, "DeleteTenant") {
		t.Fatal("must not call the API with an API-key context")
	}
}

func TestDeleteRefusesDirectAPIKeyToken(t *testing.T) {
	fake := clienttest.New()
	f := oidcFactory(t, fake)
	f.Flags.Token = "kupe_abc" // --token / KUPE_API_TOKEN beats the OIDC context
	err := run(NewCmd(f), "delete", "--confirm", "acme")
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitAuth, err)
	}
	if hasCall(fake, "DeleteTenant") {
		t.Fatal("must not call the API with an API key")
	}
}

func TestDeleteRequiresConfirmFlag(t *testing.T) {
	fake := clienttest.New()
	f := oidcFactory(t, fake)
	err := run(NewCmd(f), "delete")
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitMisuse, err)
	}
	if hasCall(fake, "DeleteTenant") {
		t.Fatal("must not call the API without --confirm")
	}
}

func TestDeleteConfirmMustMatchTenant(t *testing.T) {
	fake := clienttest.New()
	f := oidcFactory(t, fake)
	err := run(NewCmd(f), "delete", "--confirm", "acme-prod")
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitMisuse, err)
	}
	if !strings.Contains(err.Error(), `"acme-prod"`) || !strings.Contains(err.Error(), `"acme"`) {
		t.Fatalf("message should show both names: %v", err)
	}
	if hasCall(fake, "DeleteTenant") {
		t.Fatal("must not call the API on a mismatched --confirm")
	}
}

func TestDeleteAcceptedPrintsTerminating(t *testing.T) {
	fake := clienttest.New()
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := strings.TrimSpace(stdout(f)); got != "tenant/acme terminating" {
		t.Fatalf("stdout = %q", got)
	}
	if len(fake.DeleteTenantRequests) != 1 || fake.DeleteTenantRequests[0].Confirm != "acme" || fake.DeleteTenantRequests[0].Cascade {
		t.Fatalf("request = %+v; want confirm=acme cascade=false", fake.DeleteTenantRequests)
	}
	if hasCall(fake, "ListClusters") {
		t.Fatal("clusters are only listed with --cascade")
	}
	if hasCall(fake, "GetTenant") {
		t.Fatal("no polling without --wait")
	}
}

func TestDeleteCascadeListsClustersAndSendsCascade(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
	fake.Clusters["staging"] = &client.Cluster{Name: "staging", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme", "--cascade"); err != nil {
		t.Fatalf("delete --cascade: %v", err)
	}
	errOut := stderr(f)
	for _, want := range []string{"2 cluster(s)", "prod", "staging"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
	if len(fake.DeleteTenantRequests) != 1 || !fake.DeleteTenantRequests[0].Cascade {
		t.Fatalf("request = %+v; want cascade=true", fake.DeleteTenantRequests)
	}
	if strings.Contains(stdout(f), "prod") {
		t.Fatal("cluster list belongs on stderr, not stdout")
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
		{"403 owner_required", &client.APIError{StatusCode: 403, Code: "owner_required", Message: "not the owner", RequestID: "r1"}, cli.ExitAuth, "only the tenant owner", "kupe tenant get"},
		{"409 clusters_exist", &client.APIError{StatusCode: 409, Code: "clusters_exist", Message: "2 active clusters"}, cli.ExitConflict, "still has clusters", "--cascade"},
		{"409 already_terminating", &client.APIError{StatusCode: 409, Code: "ALREADY_TERMINATING", Message: "deleting"}, cli.ExitConflict, "already being deleted", "kupe tenant get"},
		{"409 unknown code", &client.APIError{StatusCode: 409, Message: "something else"}, cli.ExitConflict, "cannot delete tenant", ""},
		{"429", &client.APIError{StatusCode: 429, Message: "slow down"}, cli.ExitRateLimited, "rate limited", "retry"},
		{"400 bad confirm", &client.APIError{StatusCode: 400, Message: "confirm mismatch"}, cli.ExitMisuse, "rejected the confirmation", ""},
		{"404 gone", &client.APIError{StatusCode: 404, Message: "tenant not found"}, cli.ExitNotFound, "tenant not found", ""},
		{"401", &client.APIError{StatusCode: 401, Message: "bad token"}, cli.ExitAuth, "bad token", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.DeleteTenantErr = tt.err
			f := oidcFactory(t, fake)
			err := run(NewCmd(f), "delete", "--confirm", "acme")
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
			if strings.Contains(stdout(f), "terminating") {
				t.Fatal("must not print the success line on failure")
			}
		})
	}
}

func TestDeleteWaitPollsUntilGone(t *testing.T) {
	fake := clienttest.New()
	// First poll: still Terminating; second poll: 404 → done.
	fake.GetTenantSeq = []*client.Tenant{
		{Name: "acme", Status: &client.TenantStatus{Phase: client.PhaseTerminating}},
		nil,
	}
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme", "--wait", "--wait-timeout", "30s"); err != nil {
		t.Fatalf("delete --wait: %v", err)
	}
	if got := strings.TrimSpace(stdout(f)); got != "tenant/acme terminating" {
		t.Fatalf("stdout = %q", got)
	}
	if !strings.Contains(stderr(f), "tenant acme deleted") {
		t.Fatalf("stderr should carry the wait success line:\n%s", stderr(f))
	}
	polls := 0
	for _, c := range fake.Calls {
		if c == "GetTenant" {
			polls++
		}
	}
	if polls != 2 {
		t.Fatalf("GetTenant polls = %d; want 2", polls)
	}
}

func TestDeleteWaitTimeoutExit8(t *testing.T) {
	fake := clienttest.New()
	fake.TenantResponse = &client.Tenant{Name: "acme", Status: &client.TenantStatus{Phase: client.PhaseTerminating}}
	f := oidcFactory(t, fake)
	start := time.Now()
	err := run(NewCmd(f), "delete", "--confirm", "acme", "--wait", "--wait-timeout", "50ms")
	if code := cli.ExitCode(err); code != cli.ExitTimeout {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitTimeout, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("wait did not honour --wait-timeout")
	}
	var ce *cli.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "kupe tenant get") {
		t.Fatalf("timeout should hint at kupe tenant get: %v", err)
	}
}

func TestDeleteWaitTerminalErrorSurfaces(t *testing.T) {
	fake := clienttest.New()
	fake.TenantErr = &client.APIError{StatusCode: 401, Message: "token expired"}
	f := oidcFactory(t, fake)
	err := run(NewCmd(f), "delete", "--confirm", "acme", "--wait", "--wait-timeout", "5s")
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitAuth, err)
	}
}
