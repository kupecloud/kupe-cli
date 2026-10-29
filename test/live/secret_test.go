//go:build live

package live

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSecretLifecycle covers the full secret CRUD path:
//
//	create → list (contains it) → get → update (replace sync) → get (verify) → delete
//
// We don't actually populate the OpenBao path — the API+operator
// reconcile based on the metadata alone, and the secret will sit in the
// "Pending" phase forever without a backing KV value. That's fine for a
// CRUD smoke test; the cluster_test does end-to-end Ready waiting.
func TestSecretLifecycle(t *testing.T) {
	name := uniqueName("cli-live-secret")
	path := "kv/data/cli-live/" + name

	// Create.
	var created map[string]any
	runCLIJSON(t, &created, "secret", "create", name,
		"--path", path, "--wait=false",
	)
	if created["name"] != name {
		t.Errorf("created.name = %v; want %q", created["name"], name)
	}
	if created["secretPath"] != path {
		t.Errorf("created.secretPath = %v; want %q", created["secretPath"], path)
	}
	t.Cleanup(func() {
		_ = runCLI(t, "secret", "delete", name, "--yes")
	})

	// Listed.
	var listed []map[string]any
	runCLIJSON(t, &listed, "secret", "list")
	found := false
	for _, s := range listed {
		if s["name"] == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created secret %q not in list", name)
	}

	// Get individually.
	var got map[string]any
	runCLIJSON(t, &got, "secret", "get", name)
	if got["name"] != name {
		t.Errorf("get.name = %v; want %q", got["name"], name)
	}

	// Update — add a sync target. The spec is `cluster:namespace[:secretName]`.
	syncSpec := "test-cluster:test-ns:remote-name"
	runCLIExpectOK(t, "secret", "update", name, "--sync", syncSpec, "--wait=false")

	// Verify the sync entry came back as expected.
	var afterUpdate map[string]any
	runCLIJSON(t, &afterUpdate, "secret", "get", name)
	syncs, _ := afterUpdate["sync"].([]any)
	if len(syncs) != 1 {
		t.Fatalf("expected 1 sync target, got %d", len(syncs))
	}
	first, _ := syncs[0].(map[string]any)
	if first["cluster"] != "test-cluster" || first["namespace"] != "test-ns" || first["secretName"] != "remote-name" {
		t.Errorf("sync target wrong: %+v", first)
	}

	// Delete.
	r := runCLI(t, "secret", "delete", name, "--yes")
	if r.exitCode != 0 {
		t.Fatalf("delete exit=%d\nstderr:\n%s", r.exitCode, r.stderr)
	}

	// Confirm gone — get should now exit 4 (not-found).
	r = runCLI(t, "secret", "get", name)
	if r.exitCode != 4 {
		t.Errorf("post-delete get exit = %d; want 4 (not-found)\nstderr:\n%s", r.exitCode, r.stderr)
	}
}

// TestSecretCreateRejectsBadSyncSpec proves the parser rejects malformed
// --sync values client-side (no API round-trip).
func TestSecretCreateRejectsBadSyncSpec(t *testing.T) {
	name := uniqueName("cli-live-bad-secret")
	r := runCLI(t, "secret", "create", name,
		"--path", "kv/data/foo",
		"--sync", "this-is-not-cluster-colon-namespace",
	)
	if r.exitCode != 2 {
		t.Fatalf("exit = %d; want 2 (misuse)\nstderr:\n%s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stderr, "sync") {
		t.Errorf("stderr should mention --sync:\n%s", r.stderr)
	}
}

// TestSecretSelectedKeysAndRefreshWait exercises the additive CLI surface.
// The selected-key definition uses a nonexistent target without waiting;
// a separate no-target definition proves refresh acknowledgement and wait.
func TestSecretSelectedKeysAndRefreshWait(t *testing.T) {
	selected := uniqueName("cli-live-selected")
	var created map[string]any
	runCLIJSON(t, &created, "secret", "create", selected, "--path", "cli-live/"+selected,
		"--sync", "nonexistent-cluster:test-ns", "--sync-key", "nonexistent-cluster:test-ns:API_KEY", "--wait=false")
	t.Cleanup(func() { _ = runCLI(t, "secret", "delete", selected, "--yes", "--wait=false") })
	targets, _ := created["sync"].([]any)
	if len(targets) != 1 {
		t.Fatalf("sync targets=%v", targets)
	}
	target, _ := targets[0].(map[string]any)
	keys, _ := target["keys"].([]any)
	if len(keys) != 1 || keys[0] != "API_KEY" {
		t.Fatalf("keys=%v", keys)
	}
	runCLIExpectOK(t, "secret", "delete", selected, "--yes", "--wait=true")

	name := uniqueName("cli-live-refresh")
	var noTargets map[string]any
	runCLIJSON(t, &noTargets, "secret", "create", name, "--path", "cli-live/"+name)
	t.Cleanup(func() { _ = runCLI(t, "secret", "delete", name, "--yes", "--wait=false") })
	var refreshed map[string]any
	runCLIJSON(t, &refreshed, "secret", "refresh", name, "--wait=true")
	if refreshed["secret"] != name || refreshed["requestedAt"] == "" || refreshed["delivered"] != true {
		t.Fatalf("refresh=%v", refreshed)
	}
	runCLIExpectOK(t, "secret", "wait", name, "--for", "delivered", "--timeout", "5m")
	runCLIExpectOK(t, "secret", "delete", name, "--yes", "--wait=true")
	runCLIExpectOK(t, "secret", "wait", name, "--for", "deleted", "--timeout", "5m")
}

// TestSecretValuesLifecycle uses synthetic file input only. The builder compiles
// this fixture; the engineer runs it against the released API.
func TestSecretValuesLifecycle(t *testing.T) {
	name := uniqueName("cli-values")
	path := "cli-live/" + name
	input := filepath.Join(t.TempDir(), "values.json")
	if err := os.WriteFile(input, []byte(`{"FIELD_A":"synthetic-live-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	runCLIExpectOK(t, "secret", "create", name, "--path", path)
	t.Cleanup(func() {
		_ = runCLI(t, "secret", "delete", name, "--yes")
		_ = runCLI(t, "secret", "purge", "--path", path, "--yes")
	})
	var created struct {
		Version int64 `json:"version"`
	}
	runCLIJSON(t, &created, "secret", "values", "set", name, "--expected-version", "0", "--file", input)
	if created.Version <= 0 {
		t.Fatal("missing created version")
	}
	var updated struct {
		Version int64 `json:"version"`
	}
	runCLIJSON(t, &updated, "secret", "values", "set", name, "--expected-version", strconv.FormatInt(created.Version, 10), "--file", input)
	if updated.Version <= created.Version {
		t.Fatal("version did not advance")
	}
	stale := runCLI(t, "secret", "values", "set", name, "--expected-version", strconv.FormatInt(created.Version, 10), "--file", input)
	if stale.exitCode != 5 {
		t.Fatalf("stale version exit=%d", stale.exitCode)
	}
	var metadata map[string]any
	runCLIJSON(t, &metadata, "secret", "values", "get", name)
	if _, ok := metadata["values"]; ok {
		t.Fatal("metadata includes values")
	}
	if metadata["version"] != float64(updated.Version) {
		t.Fatal("metadata version differs")
	}
	runCLIExpectOK(t, "secret", "delete", name, "--yes")
	result := runCLI(t, "secret", "purge", "--path", path, "--yes")
	if result.exitCode != 0 {
		t.Fatalf("purge exit=%d stderr=%s", result.exitCode, result.stderr)
	}
	if strings.Contains(result.stdout+result.stderr, "synthetic-live-value") {
		t.Fatal("value leaked")
	}
}
