//go:build live

package live

import (
	"os"
	"strings"
	"testing"
)

// helloAppImage is copied by tooling from hello-kupe's committed release pin.
// Source: kupecloud/hello-kupe 254922861a9f87fa454575368ed023978a6967c8 chart/values.yaml.
const helloAppImage = "ghcr.io/kupecloud/hello-kupe@sha256:50be8d681deba388ea68d9f8ccfe398bb9b8894e951d6aff079d2d9725e60868"

// TestAppLifecycle is compiled locally; the engineer runs it on a designated
// tenant and cluster. No fixture creates or removes the destination cluster.
func TestAppLifecycle(t *testing.T) {
	cluster := os.Getenv("KUPE_LIVE_APP_CLUSTER")
	if cluster == "" {
		t.Skip("KUPE_LIVE_APP_CLUSTER is unset; app lifecycle needs a designated existing cluster")
	}
	name := uniqueName("app")
	t.Cleanup(func() {
		_ = runCLI(t, "app", "env", "update", name, "production", "--deletion-protection=false", "--wait=false")
		_ = runCLI(t, "app", "env", "delete", name, "production", "--yes")
		_ = runCLI(t, "app", "update", name, "--deletion-protection=false")
		_ = runCLI(t, "app", "delete", name, "--yes")
	})
	runCLIExpectOK(t, "app", "create", name, "--no-deletion-protection")
	runCLIExpectOK(t, "app", "env", "create", name, "production", "--cluster", cluster, "--image", helloAppImage, "--port", "8080", "--no-deletion-protection")
	var got map[string]any
	runCLIJSON(t, &got, "app", "env", "get", name, "production")
	if got["environment"] != "production" || got["cluster"] != cluster {
		t.Fatalf("environment=%v", got)
	}
	runCLIExpectOK(t, "app", "env", "update", name, "production", "--env", "LOG_LEVEL=info")
	r := runCLI(t, "cluster", "delete", cluster, "--yes")
	if r.exitCode != 5 || !strings.Contains(r.stderr, "CLUSTER_IN_USE") {
		t.Fatalf("cluster delete exit=%d stderr=%s", r.exitCode, r.stderr)
	}
	runCLIExpectOK(t, "app", "env", "delete", name, "production", "--yes")
	runCLIExpectOK(t, "app", "delete", name, "--yes")
}
