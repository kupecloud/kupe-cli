//go:build live

package live

import (
	"strings"
	"testing"
)

// TestWhoami exercises the simplest authenticated path: GET /tenants/{tenant}
// surfaced as `kupe auth whoami`. If this fails, every other test in the
// suite will fail too — by running it first we get a clear "your token is
// wrong" signal rather than a wall of unrelated red.
func TestWhoami(t *testing.T) {
	var got struct {
		Tenant      string `json:"tenant"`
		DisplayName string `json:"displayName"`
		Plan        string `json:"plan"`
	}
	runCLIJSON(t, &got, "auth", "whoami")
	if got.Tenant != testTenant {
		t.Errorf("tenant = %q; want %q", got.Tenant, testTenant)
	}
	if got.Plan == "" {
		t.Error("plan is empty in whoami output")
	}
}

// TestWhoamiTextOutput verifies the default (non-JSON) output mode emits
// the tenant name in stdout. Catches regressions where someone wires a
// new printer that swallows the resource name.
func TestWhoamiTextOutput(t *testing.T) {
	out := runCLIExpectOK(t, "auth", "whoami")
	if !strings.Contains(out, testTenant) {
		t.Errorf("text whoami missing tenant %q\nstdout:\n%s", testTenant, out)
	}
	assertNoTokenLeak(t, out)
}

// TestUnauthenticatedFailsCleanly proves a bogus token gets a typed auth
// error (exit 3), not a panic or an opaque "request failed". Important
// because this is the most common real-world failure mode.
func TestUnauthenticatedFailsCleanly(t *testing.T) {
	r := runCLI(t, "--token", "kupe_definitely-not-a-real-token", "auth", "whoami")
	if r.exitCode != 3 {
		t.Fatalf("exit = %d; want 3 (auth)\nstderr:\n%s", r.exitCode, r.stderr)
	}
}
