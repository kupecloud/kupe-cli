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
		_ = runCLI(t, "database", "update", name, "--deletion-protection=false")
		_ = runCLI(t, "database", "delete", name, "--yes")
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

// TestDatabaseLifecycle covers the database and binding command surface
// against the API:
//
//	create → list → get → update → rotate-credentials → binding add/list/get/delete → delete
//
// It does not wait for Running: whether the platform provisions PostgreSQL
// and delivers bindings is covered by the platform's own end-to-end tests.
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
	runCLIJSON(t, &updated, "db", "update", name, "--storage", "6Gi")
	if res, _ := updated["resources"].(map[string]any); res["storage"] != "6Gi" {
		t.Errorf("storage not grown: %+v", updated["resources"])
	}

	out := runCLIExpectOK(t, "db", "rotate-credentials", name, "--role", "app", "--yes")
	if !strings.Contains(out, "rotation requested") {
		t.Errorf("rotate stdout = %q", out)
	}

	// The cluster need not exist; the binding reports it until it does.
	binding := name + "-api-app"
	var bound map[string]any
	runCLIJSON(t, &bound, "db", "binding", "add", name, "--cluster", "nonexistent-cluster", "--namespace", "api", "--role", "app", "--name", binding)
	if bound["name"] != binding || bound["role"] != "app" {
		t.Fatalf("binding = %+v", bound)
	}
	var bindings []map[string]any
	runCLIJSON(t, &bindings, "db", "binding", "list", name)
	if len(bindings) != 1 || bindings[0]["name"] != binding {
		t.Fatalf("bindings = %+v", bindings)
	}
	var gotBinding map[string]any
	runCLIJSON(t, &gotBinding, "db", "binding", "get", name, binding)
	if gotBinding["namespace"] != "api" {
		t.Errorf("binding get = %+v", gotBinding)
	}
	if out := runCLIExpectOK(t, "db", "binding", "delete", name, binding, "--yes"); out != "binding/"+binding+" delete requested\n" {
		t.Errorf("binding delete stdout = %q", out)
	}
	// The delete is asynchronous: the binding stays readable until the
	// platform has removed its Service and Secret.
	waitForBindingGone(t, name, binding, 2*time.Minute)

	if out := runCLIExpectOK(t, "db", "delete", name, "--yes"); out != "database/"+name+" delete requested\n" {
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
