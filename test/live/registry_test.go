//go:build live

package live

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// waitForExit polls kupe with args until it exits want, failing the test
// after timeout.
func waitForExit(t *testing.T, want int, timeout time.Duration, args ...string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		r := runCLI(t, args...)
		if r.exitCode == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("kupe %v exit = %d; want %d within %s\nstderr:\n%s", args, r.exitCode, want, timeout, r.stderr)
		}
		time.Sleep(5 * time.Second)
	}
}

// TestRegistryLifecycle covers the registry command surface against the
// API:
//
//	get (or create) → token create / list / revoke → login --no-store -o json →
//	repo list → (KUPE_LIVE_CLUSTER=1) connect / connection get / disconnect →
//	delete refused while protected → update --deletion-protection=false → delete
//
// The registry is reused when the testing tenant already has one and
// deleted at the end only if this run created it. Pushing and pulling
// images is covered by kupe-tests, which drives Docker with the credentials
// this CLI writes.
func TestRegistryLifecycle(t *testing.T) {
	var reg map[string]any
	created := false
	if r := runCLI(t, "registry", "get", "-o", "json"); r.exitCode == 4 {
		runCLIJSON(t, &reg, "registry", "create", "--wait-timeout", "15m")
		created = true
		t.Cleanup(func() {
			_ = runCLI(t, "registry", "update", "--deletion-protection=false")
			_ = runCLI(t, "registry", "delete", "--yes", "--wait=false")
		})
	} else {
		runCLIJSON(t, &reg, "registry", "get")
	}
	if reg["name"] != "registry" {
		t.Fatalf("registry = %+v", reg)
	}

	t.Run("access token", func(t *testing.T) {
		name := uniqueName("cli-live-tok")
		var tok map[string]any
		runCLIJSON(t, &tok, "registry", "token", "create", "--name", name, "--access", "pull", "--expires-in-days", "1")
		id, _ := tok["id"].(string)
		t.Cleanup(func() { _ = runCLI(t, "registry", "token", "revoke", id, "--yes") })
		secret, _ := tok["secret"].(string)
		if !strings.HasPrefix(id, "rt-") || !strings.HasPrefix(secret, "kupecr_") || tok["phase"] != "Ready" ||
			!strings.Contains(fmt.Sprint(tok["username"]), "+tok-") {
			t.Fatalf("created token = id %q phase %v username %v", id, tok["phase"], tok["username"])
		}
		list := runCLIExpectOK(t, "registry", "token", "list", "--kind", "token", "-o", "json")
		if !strings.Contains(list, id) || strings.Contains(list, secret) || strings.Contains(list, `"secret"`) {
			t.Fatal("token list is missing the token or carries a secret")
		}
		runCLIExpectOK(t, "registry", "token", "revoke", id, "--yes")
		waitForExit(t, 4, 2*time.Minute, "registry", "token", "get", id)
	})

	t.Run("login --no-store -o json", func(t *testing.T) {
		r := runCLI(t, "registry", "login", "--no-store", "-o", "json")
		if strings.HasPrefix(apiToken, "kupe_") {
			if r.exitCode != 3 || !strings.Contains(r.stderr, "kupe auth login") {
				t.Fatalf("an API key login exit = %d; want 3\nstderr:\n%s", r.exitCode, r.stderr)
			}
			return
		}
		if r.exitCode != 0 {
			t.Fatalf("login exit = %d\nstderr:\n%s", r.exitCode, r.stderr)
		}
		for _, key := range []string{`"server"`, `"username"`, `"password": "kupecr_`, `"expiresAt"`} {
			if !strings.Contains(r.stdout, key) {
				t.Fatalf("login json missing %s", key)
			}
		}
		_ = runCLI(t, "registry", "logout", "--docker-config", t.TempDir())
	})

	t.Run("repositories", func(t *testing.T) {
		var repos []map[string]any
		runCLIJSON(t, &repos, "registry", "repo", "list", "--all")
	})

	t.Run("cluster connection", func(t *testing.T) {
		if os.Getenv("KUPE_LIVE_CLUSTER") != "1" {
			t.Skip("set KUPE_LIVE_CLUSTER=1 to enable the cluster connection test")
		}
		cluster := uniqueName("cli-live-conn")
		t.Cleanup(func() { _ = runCLI(t, "registry", "disconnect", cluster, "--yes") })
		// The cluster need not exist: the connection stays Pending.
		var conn map[string]any
		runCLIJSON(t, &conn, "registry", "connect", cluster, "--push-namespace", "ci", "--wait=false")
		if conn["cluster"] != cluster {
			t.Fatalf("connection = %+v", conn)
		}
		runCLIJSON(t, &conn, "registry", "connection", "get", cluster)
		runCLIExpectOK(t, "registry", "disconnect", cluster, "--yes")
		waitForExit(t, 4, 5*time.Minute, "registry", "connection", "get", cluster)
	})

	if !created {
		return
	}
	if r := runCLI(t, "registry", "delete", "--yes"); r.exitCode != 5 || !strings.Contains(r.stderr, "--deletion-protection=false") {
		t.Fatalf("delete of a protected registry exit = %d; want 5\nstderr:\n%s", r.exitCode, r.stderr)
	}
	runCLIExpectOK(t, "registry", "update", "--deletion-protection=false")
	runCLIExpectOK(t, "registry", "delete", "--yes", "--wait-timeout", "15m")
}
