//go:build live

package live

import "testing"

// TestTenantGet covers `kupe tenant get` end-to-end. Same backend call as
// whoami but exercises the tenant noun's printer + the cmd/tenant flag
// surface (which has its own validation paths).
func TestTenantGet(t *testing.T) {
	var got map[string]any
	runCLIJSON(t, &got, "tenant", "get")
	if got["name"] != testTenant {
		t.Errorf("name = %v; want %q", got["name"], testTenant)
	}
	if got["plan"] == "" {
		t.Error("plan missing from tenant get response")
	}
}
