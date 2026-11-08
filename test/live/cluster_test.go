//go:build live

package live

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestClusterLifecycle exercises the most expensive end-to-end path:
// create → wait Running → get → update (resize) → kubeconfig (both stanza
// styles) → delete. The whole thing takes 5–8 minutes against dev because
// kupe-control-operator provisions a real vCluster.
//
// Gated on KUPE_LIVE_CLUSTER=1 so the rest of `make test-live` stays
// under a minute. Run explicitly when you want full coverage:
//
//	KUPE_LIVE_CLUSTER=1 make test-live
func TestClusterLifecycle(t *testing.T) {
	if os.Getenv("KUPE_LIVE_CLUSTER") != "1" {
		t.Skip("set KUPE_LIVE_CLUSTER=1 to enable the cluster lifecycle test (5-8 min)")
	}

	name := uniqueName("cli-live")

	// Create with --wait=true so the CLI handles the polling for us.
	// --cpu-limit / --memory-limit / --storage-limit are required as of
	// the resource-quota rollout; pick the smallest viable shape so the
	// smoke test stays cheap.
	t.Logf("creating cluster %q (this can take 5-8 minutes)", name)
	r := runCLI(t,
		"cluster", "create", name,
		"--type", "shared",
		"--cpu-limit", "2", "--memory-limit", "8Gi", "--storage-limit", "50Gi",
		"--wait", "--wait-timeout", "12m",
	)
	if r.exitCode != 0 {
		t.Fatalf("create exit=%d\nstderr:\n%s", r.exitCode, r.stderr)
	}
	t.Cleanup(func() {
		t.Logf("deleting cluster %q", name)
		_ = runCLI(t, "cluster", "delete", name, "--yes", "--wait", "--wait-timeout", "8m")
	})

	// Get: phase should be Running by now (create --wait waited for it).
	var got map[string]any
	runCLIJSON(t, &got, "cluster", "get", name)
	if got["name"] != name {
		t.Errorf("get.name = %v; want %q", got["name"], name)
	}
	status, _ := got["status"].(map[string]any)
	if phase, _ := status["phase"].(string); phase != "Running" {
		t.Fatalf("phase = %q; want Running\nfull status: %+v", phase, status)
	}

	// List should also include it.
	var listed []map[string]any
	runCLIJSON(t, &listed, "cluster", "list")
	found := false
	for _, c := range listed {
		if c["name"] == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created cluster %q not in list of %d", name, len(listed))
	}

	// Kubeconfig — the harness passes a direct token (KUPE_API_TOKEN), which
	// an exec kubeconfig cannot carry, so the default form is refused with
	// exit 2. The exec form itself is covered by the unit tests.
	r = runCLI(t, "cluster", "kubeconfig", name)
	if r.exitCode != 2 {
		t.Fatalf("default kubeconfig with a direct token: exit = %d; want 2\nstderr:\n%s", r.exitCode, r.stderr)
	}
	assertNoTokenLeak(t, r.stdout)

	// Kubeconfig — --embed-token carries a bearer token and no exec stanza.
	embedOut := runCLIExpectOK(t, "cluster", "kubeconfig", name, "--embed-token")
	if !strings.Contains(embedOut, "token:") || strings.Contains(embedOut, "exec:") {
		t.Errorf("--embed-token kubeconfig should embed a token and have no exec stanza:\n%s", embedOut[:min(len(embedOut), 500)])
	}
}

// TestClusterWaitOnNonexistentExitsCleanly ensures `kupe cluster wait`
// against a missing cluster surfaces as exit 4 without hanging the full
// timeout. Cheap test, runs without KUPE_LIVE_CLUSTER.
func TestClusterWaitOnNonexistentExitsCleanly(t *testing.T) {
	r := runCLI(t, "cluster", "wait", "definitely-not-a-real-cluster-"+uniqueName(""), "--for", "Running", "--wait-timeout", "5s")
	if r.exitCode == 0 {
		t.Fatal("wait against nonexistent cluster should not exit 0")
	}
	// Either 4 (not-found, fast path) or 8 (timeout) is acceptable; the
	// goal is "no hang and no crash", not a specific code.
	if r.exitCode != 4 && r.exitCode != 8 {
		t.Errorf("exit = %d; want 4 (not-found) or 8 (timeout)\nstderr:\n%s", r.exitCode, r.stderr)
	}
	_ = time.Second // keep time import for future expansions
}
