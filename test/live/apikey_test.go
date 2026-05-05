//go:build live

package live

import (
	"strings"
	"testing"
)

// TestAPIKeyLifecycle creates a transient API key, lists it back, and
// deletes it. Verifies:
//   - create returns a populated kupe_... token
//   - the new key shows up in list
//   - delete leaves no trace
//
// The token is captured from stdout but never logged or compared against
// the apiToken — it's only used to assert non-empty.
func TestAPIKeyLifecycle(t *testing.T) {
	name := uniqueName("cli-live")

	// Create. JSON shape from internal/cmd/apikey/create.go:createResponse.
	var created struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Role  string `json:"role"`
		Token string `json:"token"`
	}
	runCLIJSON(t, &created, "apikey", "create",
		"--name", name,
		"--role", "readonly",
		"--expires-at", "1h",
	)
	if created.ID == "" {
		t.Fatal("created.ID is empty")
	}
	if !strings.HasPrefix(created.Token, "kupe_") {
		t.Fatalf("token does not look like a kupe key: %q", created.Token)
	}
	if created.Name != name {
		t.Errorf("created.Name = %q; want %q", created.Name, name)
	}
	t.Cleanup(func() {
		// Best-effort delete; the explicit delete below should already
		// have run in the happy path.
		_ = runCLI(t, "apikey", "delete", created.ID)
	})

	// List should contain our new key.
	var listed []map[string]any
	runCLIJSON(t, &listed, "apikey", "list")
	found := false
	for _, k := range listed {
		if k["id"] == created.ID {
			found = true
			if k["name"] != name {
				t.Errorf("listed key name=%v; want %q", k["name"], name)
			}
			break
		}
	}
	if !found {
		t.Fatalf("created key %q not found in list of %d keys", created.ID, len(listed))
	}

	// Delete.
	r := runCLI(t, "apikey", "delete", created.ID)
	if r.exitCode != 0 {
		t.Fatalf("delete exit=%d\nstderr:\n%s", r.exitCode, r.stderr)
	}

	// Confirm gone.
	var afterDelete []map[string]any
	runCLIJSON(t, &afterDelete, "apikey", "list")
	for _, k := range afterDelete {
		if k["id"] == created.ID {
			t.Fatalf("key %q still present after delete", created.ID)
		}
	}
}

// TestAPIKeyCreateRejectsInvalidRole proves the client-side validation
// fires before any HTTP call. Exit 2 (misuse) on stderr containing the
// invalid value.
func TestAPIKeyCreateRejectsInvalidRole(t *testing.T) {
	r := runCLI(t, "apikey", "create", "--name", "should-not-create", "--role", "superuser")
	if r.exitCode != 2 {
		t.Fatalf("exit = %d; want 2 (misuse)\nstderr:\n%s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stderr, "superuser") {
		t.Errorf("stderr should mention the bad role:\n%s", r.stderr)
	}
}
