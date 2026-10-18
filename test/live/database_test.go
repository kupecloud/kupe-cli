//go:build live

package live

import (
	"strings"
	"testing"
	"time"
)

// smallestDatabase is the smallest shape the platform floors admit.
var smallestDatabase = []string{"--cpu", "250m", "--memory", "512Mi", "--storage", "5Gi"}

// cleanupDatabase turns deletion protection off and deletes the database,
// so a failed run never leaves a protected database in the testing tenant.
func cleanupDatabase(t *testing.T, name string) {
	t.Cleanup(func() {
		_ = runCLI(t, "database", "update", name, "--deletion-protection=false", "--wait=false")
		_ = runCLI(t, "database", "delete", name, "--yes", "--wait=false")
	})
}

// waitForBindingGone polls "db binding get" until it exits 4 (not found),
// failing the test if the binding is still there after timeout.
func waitForBindingGone(t *testing.T, database, binding string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		r := runCLI(t, "db", "binding", "get", database, binding)
		if r.exitCode == 4 {
			return
		}
		if r.exitCode != 0 || time.Now().After(deadline) {
			t.Errorf("deleted binding get exit = %d; want 4 within %s\nstderr:\n%s", r.exitCode, timeout, r.stderr)
			return
		}
		time.Sleep(5 * time.Second)
	}
}

// sameNamespaces reports whether a binding's JSON lists exactly want, in
// order.
func sameNamespaces(binding map[string]any, want ...string) bool {
	got, _ := binding["namespaces"].([]any)
	if len(got) != len(want) {
		return false
	}
	for i, ns := range got {
		if ns != want[i] {
			return false
		}
	}
	return true
}

// TestDatabaseLifecycle covers the database and binding command surface
// against the API:
//
//	create → list → get → update → rotate-credentials (each role) →
//	binding add (readwrite into two namespaces, readonly) / list / update / get / delete → delete
//
// It does not wait for Running, so every command that would wait passes
// --wait=false: whether the platform provisions PostgreSQL and delivers
// bindings is covered by the platform's own end-to-end tests.
func TestDatabaseLifecycle(t *testing.T) {
	name := uniqueName("cli-live-db")
	cleanupDatabase(t, name)

	var created map[string]any
	runCLIJSON(t, &created, append([]string{"database", "create", name, "--no-deletion-protection", "--wait=false"}, smallestDatabase...)...)
	if created["name"] != name || created["engine"] != "postgres" || created["deletionProtection"] != false {
		t.Fatalf("created = %+v", created)
	}

	var listed []map[string]any
	runCLIJSON(t, &listed, "db", "list")
	found := false
	for _, db := range listed {
		if db["name"] == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("database %q not in list", name)
	}

	var got map[string]any
	runCLIJSON(t, &got, "db", "get", name)
	if got["version"] == "" {
		t.Errorf("expected the platform default version to be applied, got %+v", got)
	}

	var updated map[string]any
	runCLIJSON(t, &updated, "db", "update", name, "--storage", "6Gi", "--wait=false")
	if res, _ := updated["resources"].(map[string]any); res["storage"] != "6Gi" {
		t.Errorf("storage not grown: %+v", updated["resources"])
	}

	for _, role := range []string{"owner", "readwrite", "readonly"} {
		out := runCLIExpectOK(t, "db", "rotate-credentials", name, "--role", role, "--yes", "--wait=false")
		if !strings.Contains(out, "rotation requested") {
			t.Errorf("rotate %s stdout = %q", role, out)
		}
	}

	// The cluster need not exist; the binding stays Pending until it does,
	// so the add does not wait for Ready.
	binding := name + "-api-readwrite"
	var bound map[string]any
	runCLIJSON(t, &bound, "db", "binding", "add", name, "--cluster", "nonexistent-cluster",
		"--namespace", "api", "--namespace", "jobs", "--role", "readwrite", "--name", binding, "--wait=false")
	if bound["name"] != binding || bound["role"] != "readwrite" || !sameNamespaces(bound, "api", "jobs") {
		t.Fatalf("binding = %+v", bound)
	}
	// The default name is DATABASE-CLUSTER-ROLE.
	readonly := name + "-nonexistent-cluster-readonly"
	var boundReadonly map[string]any
	runCLIJSON(t, &boundReadonly, "db", "binding", "add", name, "--cluster", "nonexistent-cluster", "--namespace", "reports", "--role", "readonly", "--wait=false")
	if boundReadonly["name"] != readonly || boundReadonly["role"] != "readonly" || !sameNamespaces(boundReadonly, "reports") {
		t.Fatalf("readonly binding = %+v", boundReadonly)
	}
	var bindings []map[string]any
	runCLIJSON(t, &bindings, "db", "binding", "list", name)
	if len(bindings) != 2 {
		t.Fatalf("bindings = %+v", bindings)
	}

	var changed map[string]any
	runCLIJSON(t, &changed, "db", "binding", "update", name, binding, "--add-namespace", "workers", "--remove-namespace", "jobs", "--wait=false")
	if !sameNamespaces(changed, "api", "workers") {
		t.Fatalf("updated binding = %+v", changed)
	}
	var gotBinding map[string]any
	runCLIJSON(t, &gotBinding, "db", "binding", "get", name, binding)
	if !sameNamespaces(gotBinding, "api", "workers") {
		t.Errorf("binding get = %+v", gotBinding)
	}
	for _, b := range []string{binding, readonly} {
		if out := runCLIExpectOK(t, "db", "binding", "delete", name, b, "--yes"); out != "binding/"+b+" delete requested\n" {
			t.Errorf("binding delete stdout = %q", out)
		}
	}
	// The delete is asynchronous: the binding stays readable until the
	// platform has removed its Services and Secrets.
	waitForBindingGone(t, name, binding, 2*time.Minute)
	waitForBindingGone(t, name, readonly, 2*time.Minute)

	if out := runCLIExpectOK(t, "db", "delete", name, "--yes", "--wait=false"); out != "database/"+name+" delete requested\n" {
		t.Errorf("delete stdout = %q", out)
	}
}

// TestDatabaseDeletionProtection checks a protected database is refused
// with exit 5 and a hint naming the command that turns protection off.
func TestDatabaseDeletionProtection(t *testing.T) {
	name := uniqueName("cli-live-db-protected")
	cleanupDatabase(t, name)

	runCLIExpectOK(t, append([]string{"database", "create", name, "--wait=false"}, smallestDatabase...)...)
	r := runCLI(t, "database", "delete", name, "--yes")
	if r.exitCode != 5 {
		t.Fatalf("protected delete exit = %d; want 5\nstderr:\n%s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stderr, "--deletion-protection=false") {
		t.Errorf("stderr should hint how to turn protection off:\n%s", r.stderr)
	}
}

// TestDatabaseCreateRejectsBadQuantities proves malformed resources are
// refused client-side (exit 2) without an API round trip.
func TestDatabaseCreateRejectsBadQuantities(t *testing.T) {
	r := runCLI(t, "database", "create", uniqueName("cli-live-bad-db"), "--cpu", "250m", "--memory", "512", "--storage", "5Gi")
	if r.exitCode != 2 {
		t.Fatalf("exit = %d; want 2 (misuse)\nstderr:\n%s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stderr, "--memory") {
		t.Errorf("stderr should name --memory:\n%s", r.stderr)
	}
}
