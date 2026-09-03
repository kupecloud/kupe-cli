//go:build live

package live

import (
	"os"
	"strings"
	"testing"
)

// TestTenantDeleteRefusesWrongConfirm is safe against the shared testing
// tenant: the CLI rejects a mismatched --confirm locally (exit 2) before any
// request is made. With an API-key token the OIDC gate fires first instead
// (exit 3) — both are local refusals, so the test accepts whichever applies.
func TestTenantDeleteRefusesWrongConfirm(t *testing.T) {
	r := runCLI(t, "tenant", "delete", "--confirm", testTenant+"-not-me")
	wantExit := 2
	if strings.HasPrefix(apiToken, "kupe_") {
		wantExit = 3
	}
	if r.exitCode != wantExit {
		t.Fatalf("exit = %d; want %d\nstderr:\n%s", r.exitCode, wantExit, r.stderr)
	}
}

// TestTenantDeleteLifecycle deletes a throwaway tenant end to end:
// delete --cascade --wait, then tenant get → 404 (exit 4). It is gated on
// KUPE_LIVE_DELETE_TENANT=<name> because the CLI cannot create tenants —
// provision one via signup first (kupe-tests has the fixture) and export
// its name; KUPE_API_TOKEN must be an OIDC access token for that tenant's
// owner (an API key is refused with exit 3, which is asserted below).
//
//	KUPE_LIVE_DELETE_TENANT=cli-del-1234 make test-live
func TestTenantDeleteLifecycle(t *testing.T) {
	name := os.Getenv("KUPE_LIVE_DELETE_TENANT")
	if name == "" {
		t.Skip("set KUPE_LIVE_DELETE_TENANT=<throwaway tenant> to enable the tenant deletion test")
	}
	if name == testTenant {
		t.Fatalf("refusing to delete the shared testing tenant %q", testTenant)
	}
	if strings.HasPrefix(apiToken, "kupe_") {
		r := runCLI(t, "tenant", "delete", "--confirm", name, "--tenant", name)
		if r.exitCode != 3 {
			t.Fatalf("API-key token: exit = %d; want 3\nstderr:\n%s", r.exitCode, r.stderr)
		}
		t.Skip("KUPE_API_TOKEN is an API key; the deletion itself needs the owner's OIDC token")
	}

	r := runCLI(t, "tenant", "delete", "--confirm", name, "--tenant", name, "--cascade", "--wait", "--wait-timeout", "25m")
	if r.exitCode != 0 {
		t.Fatalf("tenant delete exit = %d\nstdout:\n%s\nstderr:\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "tenant/"+name+" terminating") {
		t.Errorf("stdout missing the terminating line:\n%s", r.stdout)
	}

	get := runCLI(t, "tenant", "get", "--tenant", name)
	if get.exitCode != 4 {
		t.Fatalf("tenant get after delete: exit = %d; want 4\nstdout:\n%s\nstderr:\n%s", get.exitCode, get.stdout, get.stderr)
	}
}
