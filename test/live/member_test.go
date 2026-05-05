//go:build live

package live

import "testing"

// TestMemberList exercises the read path. We don't add or remove members
// in live tests — those mutate org state that's shared with humans and
// other test runs, and the API restricts who can be added (must be a
// known Authentik user). A list smoke test covers the wire format,
// printer, and auth path; that's enough.
func TestMemberList(t *testing.T) {
	var listed []map[string]any
	runCLIJSON(t, &listed, "member", "list")
	if len(listed) == 0 {
		t.Fatal("member list returned 0 — testing tenant should always have at least one admin")
	}
	for i, m := range listed {
		if m["email"] == "" {
			t.Errorf("member[%d] missing email", i)
		}
		role, _ := m["role"].(string)
		if role != "admin" && role != "readonly" {
			t.Errorf("member[%d] role=%q; want admin|readonly", i, role)
		}
	}
}
