package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	fake.ClustersByName["prod"] = &client.Cluster{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
	fake.ClustersByName["staging"] = &client.Cluster{Name: "staging", Status: &client.ClusterStatus{Phase: client.PhaseRunning}}
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme", "--cascade"); err != nil {
		t.Fatalf("delete --cascade: %v", err)
	}
	errOut := stderr(f)
	for _, want := range []string{"2 cluster(s)", "prod", "staging", "Not delete databases"} {
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

// --cascade can delete some clusters and then stop at one an app environment
// still uses. The warning says so before the request is sent, and the
// request itself is unchanged.
func TestDeleteCascadeWarnsItCanStopPartWay(t *testing.T) {
	const warning = "  - Stop at the first cluster an app environment still uses. Clusters deleted before it stay deleted, so delete your apps first (kupe app list).\n"
	fake := clienttest.New()
	fake.ClustersByName["prod"] = &client.Cluster{Name: "prod"}
	fake.DeleteTenantErr = &client.APIError{StatusCode: 409, Code: "cluster_in_use", Message: "cluster prod is used by app environment shop/production"}
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme", "--cascade"); cli.ExitCode(err) != cli.ExitConflict {
		t.Fatalf("exit = %d (%v)", cli.ExitCode(err), err)
	}
	if !strings.Contains(stderr(f), "      prod\n"+warning) {
		t.Fatalf("stderr should warn after the cluster list:\n%s", stderr(f))
	}
	if len(fake.DeleteTenantRequests) != 1 || !fake.DeleteTenantRequests[0].Cascade {
		t.Fatalf("request = %+v; want one cascade=true request", fake.DeleteTenantRequests)
	}

	for _, args := range [][]string{{"delete", "--confirm", "acme", "--cascade"}, {"delete", "--confirm", "acme"}} {
		fake := clienttest.New()
		if args[len(args)-1] != "--cascade" {
			fake.ClustersByName["prod"] = &client.Cluster{Name: "prod"}
		}
		f := oidcFactory(t, fake)
		if err := run(NewCmd(f), args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(stderr(f), "app environment") {
			t.Fatalf("%v: no clusters to stop at, or no cascade, needs no warning:\n%s", args, stderr(f))
		}
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
		{"403 owner_required", &client.APIError{StatusCode: 403, Code: "owner_required", Message: "only the tenant owner (contactEmail) can delete the tenant", RequestID: "r1"}, cli.ExitAuth,
			"only the tenant owner (contactEmail) can delete the tenant (request-id: r1)", "kupe tenant get"},
		{"409 clusters_exist", &client.APIError{StatusCode: 409, Code: "clusters_exist", Message: "tenant has active clusters: prod; delete them first or set cascade=true"}, cli.ExitConflict,
			"tenant has active clusters: prod; delete them first or set cascade=true", "--cascade"},
		{"409 databases_exist", &client.APIError{StatusCode: 409, Code: "databases_exist", Message: "tenant has databases: orders; delete them first (cascade does not delete databases)"}, cli.ExitConflict,
			"tenant has databases: orders; delete them first (cascade does not delete databases)", "kupe db list\n  kupe db update NAME --deletion-protection=false\n  kupe db delete NAME"},
		{"409 registries_exist", &client.APIError{StatusCode: 409, Code: "registries_exist", Message: "tenant has a registry: acme; delete it first (deleting a registry deletes every image in it)"}, cli.ExitConflict,
			"tenant has a registry: acme; delete it first (deleting a registry deletes every image in it)", "kupe registry update --deletion-protection=false\n  kupe registry delete"},
		{"409 already_terminating", &client.APIError{StatusCode: 409, Code: "ALREADY_TERMINATING", Message: `tenant "acme" is already terminating`}, cli.ExitConflict, `tenant "acme" is already terminating`, "kupe tenant get"},
		{"409 no message", &client.APIError{StatusCode: 409, Code: "clusters_exist"}, cli.ExitConflict, `tenant "acme" still has clusters: Conflict`, "--cascade"},
		{"409 unknown code", &client.APIError{StatusCode: 409, Message: "something else"}, cli.ExitConflict, `cannot delete tenant "acme": something else`, ""},
		{"429", &client.APIError{StatusCode: 429, Message: "too many tenant deletion requests; try again later"}, cli.ExitRateLimited, "too many tenant deletion requests; try again later", "retry"},
		{"400 bad confirm", &client.APIError{StatusCode: 400, Message: "confirm mismatch"}, cli.ExitMisuse, "the API rejected the confirmation: confirm mismatch", ""},
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
			// The whole message is pinned so a refusal is never printed twice.
			if err.Error() != tt.wantMsg {
				t.Fatalf("message %q, want %q", err.Error(), tt.wantMsg)
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

func TestDeleteHelpSaysCascadeNeverDeletesDatabases(t *testing.T) {
	cmd := newDeleteCmd(oidcFactory(t, clienttest.New()))
	if usage := cmd.Flag("cascade").Usage; !strings.Contains(usage, "never databases") {
		t.Errorf("--cascade usage = %q; want it to say databases are never deleted", usage)
	}
	if long := strings.Join(strings.Fields(cmd.Long), " "); !strings.Contains(long, "--cascade never deletes databases") {
		t.Errorf("Long text should say --cascade never deletes databases:\n%s", cmd.Long)
	}
}

func TestDeleteWarningListsManualBackups(t *testing.T) {
	fake := clienttest.New()
	fake.Backups["orders-db-before-migration"] = &client.Backup{Name: "orders-db-before-migration", Database: "orders-db",
		Status: &client.BackupStatus{Phase: client.PhaseCompleted, CompletedAt: "2026-10-30T14:17:00Z", SizeBytes: 1610612736, DatabaseDeleted: true}}
	fake.Backups["users-db-20261030-150000"] = &client.Backup{Name: "users-db-20261030-150000", Database: "users-db",
		Status: &client.BackupStatus{Phase: "Running"}}
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	errOut := stderr(f)
	for _, want := range []string{
		"  - Delete 2 manual database backup(s), which cannot be restored afterwards:\n",
		"      orders-db-before-migration (orders-db, completed 2026-10-30T14:17:00Z, 1.5 GiB)\n",
		"      users-db-20261030-150000 (users-db, Running)\n",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
	if !hasCall(fake, "ListBackups:") || len(fake.DeleteTenantRequests) != 1 {
		t.Fatalf("calls = %v; want the list, then the delete", fake.Calls)
	}
	if strings.Contains(stdout(f), "orders-db") {
		t.Fatal("the backup list belongs on stderr, not stdout")
	}
}

func TestDeleteWarningWithoutManualBackups(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, unknownRoute := client.New(srv.URL, "acme", "kupe_test", "kupe-cli-test/0.0.0").ListBackups(context.Background(), "")
	if !client.IsUnknownRoute(unknownRoute) {
		t.Fatalf("want an unknown-route error, got %v", unknownRoute)
	}
	tests := []struct {
		name string
		err  error
	}{
		{"none exist", nil},
		{"the platform has no manual backups", &client.APIError{StatusCode: 404, Code: client.DatabaseCodeBackupsUnavailable,
			Message: "backups are not available on this platform"}},
		{"a kupe-api without manual backups", unknownRoute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.ListBackupsErr = tt.err
			f := oidcFactory(t, fake)
			if err := run(NewCmd(f), "delete", "--confirm", "acme"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if strings.Contains(stderr(f), "backup") {
				t.Errorf("stderr mentions backups though there are none:\n%s", stderr(f))
			}
			if len(fake.DeleteTenantRequests) != 1 {
				t.Fatal("the tenant must still be deleted")
			}
		})
	}
}

func TestDeleteWarningWhenTheBackupListFails(t *testing.T) {
	fake := clienttest.New()
	fake.ListBackupsErr = &client.APIError{StatusCode: 500, Message: "internal server error"}
	f := oidcFactory(t, fake)
	if err := run(NewCmd(f), "delete", "--confirm", "acme"); err != nil {
		t.Fatalf("a failed list must not block the delete: %v", err)
	}
	if want := "  - Delete every manual database backup, which cannot be restored afterwards. The list of them could not be read: "; !strings.Contains(stderr(f), want) {
		t.Errorf("stderr missing %q:\n%s", want, stderr(f))
	}
	if len(fake.DeleteTenantRequests) != 1 {
		t.Fatal("the tenant must still be deleted")
	}
}
