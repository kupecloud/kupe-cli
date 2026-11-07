//go:build live

package live

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// helloAppImage is copied by tooling from hello-kupe's committed release pin.
// Source: kupecloud/hello-kupe 254922861a9f87fa454575368ed023978a6967c8 chart/values.yaml.
const helloAppImage = "ghcr.io/kupecloud/hello-kupe@sha256:50be8d681deba388ea68d9f8ccfe398bb9b8894e951d6aff079d2d9725e60868"

// Version copied from the same committed chart pin, before its @sha256 suffix.
const helloAppTag = "ghcr.io/kupecloud/hello-kupe:1.8.3"

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
	runCLIExpectOK(t, "app", "env", "update", name, "production", "--set-env", "LOG_LEVEL=info")
	deployed := runCLI(t, "app", "deploy", name, "production", "--image", helloAppTag, "--set-env", "LIVE_RELEASE=tag", "--yes", "-o", "json")
	assertNoTokenLeak(t, deployed.stdout+deployed.stderr)
	if deployed.exitCode != 0 || !strings.Contains(deployed.stderr, helloAppTag+" → "+helloAppImage) {
		t.Fatalf("tag deploy exit=%d stdout=%s stderr=%s", deployed.exitCode, deployed.stdout, deployed.stderr)
	}
	runCLIExpectOK(t, "app", "wait", name, "production")
	var before []client.AppRelease
	runCLIJSON(t, &before, "app", "releases", name, "production")
	if len(before) == 0 || before[0].Status.Phase != "Succeeded" {
		t.Fatalf("deployment releases=%+v", before)
	}
	runCLIExpectOK(t, "app", "retry", name, "production", "--yes")
	var retried []client.AppRelease
	runCLIJSON(t, &retried, "app", "releases", name, "production")
	if len(retried) == 0 || retried[0].Number <= before[0].Number || retried[0].Trigger != "Retry" || retried[0].Status.Phase != "Succeeded" {
		t.Fatalf("retry releases=%+v", retried)
	}
	failed := runCLI(t, "app", "deploy", name, "production", "--image", helloAppImage, "--port", "1", "--yes")
	assertNoTokenLeak(t, failed.stdout+failed.stderr)
	if failed.exitCode != 1 || !strings.Contains(failed.stderr, "failed") {
		t.Fatalf("failed deploy exit=%d stderr=%s", failed.exitCode, failed.stderr)
	}
	// Failed is terminal immediately. Recovery is an independent operation; wait
	// for its evidence before asserting the stronger previous-serving message.
	deadline := time.Now().Add(15 * time.Minute)
	for {
		var current client.AppEnvironment
		runCLIJSON(t, &current, "app", "env", "get", name, "production")
		if current.Status != nil && current.Status.Recovery != nil && current.Status.Recovery.Phase == "Succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovery did not succeed after the failed readiness rollout")
		}
		time.Sleep(5 * time.Second)
	}
	failed = runCLI(t, "app", "wait", name, "production")
	if failed.exitCode != 1 || !strings.Contains(failed.stderr, "previous release is serving again") {
		t.Fatalf("recovered wait exit=%d stderr=%s", failed.exitCode, failed.stderr)
	}
	r := runCLI(t, "cluster", "delete", cluster, "--yes")
	if r.exitCode != 5 || !strings.Contains(r.stderr, "CLUSTER_IN_USE") {
		t.Fatalf("cluster delete exit=%d stderr=%s", r.exitCode, r.stderr)
	}
	runCLIExpectOK(t, "app", "env", "delete", name, "production", "--yes")
	runCLIExpectOK(t, "app", "delete", name, "--yes")
}
