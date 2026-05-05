//go:build live

package live

import (
	"testing"
)

// TestPlanList exercises the unauthenticated /plans endpoint via
// `kupe plan list`. This path bypasses the bearer token, so we run it
// without our usual env auth and verify we still get a populated catalog.
func TestPlanList(t *testing.T) {
	var got []map[string]any
	runCLIJSON(t, &got, "plan", "list")
	if len(got) == 0 {
		t.Fatal("plan list returned 0 plans — kupe-api should always seed a catalog")
	}
	for i, p := range got {
		if p["name"] == "" {
			t.Errorf("plan[%d] missing name", i)
		}
	}
}

// TestPlanGet pulls a single plan by name. Uses the first name from the
// list above so the test is self-bootstrapping (no hard-coded plan slug).
func TestPlanGet(t *testing.T) {
	var listed []map[string]any
	runCLIJSON(t, &listed, "plan", "list")
	if len(listed) == 0 {
		t.Skip("no plans in catalog to fetch")
	}
	name, _ := listed[0]["name"].(string)
	if name == "" {
		t.Skip("first plan has no name; skipping plan get test")
	}

	var got map[string]any
	runCLIJSON(t, &got, "plan", "get", name)
	if got["name"] != name {
		t.Errorf("plan get returned name=%v; want %q", got["name"], name)
	}
}
