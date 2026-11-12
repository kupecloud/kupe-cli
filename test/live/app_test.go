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
	t.Cleanup(func() { cleanupApp(t, name, "production") })
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
	// The CLI prints the server's message, never its error code, so assert the
	// exit (5, conflict) and that the message names the blocking environment.
	if r.exitCode != 5 || !strings.Contains(r.stderr, "used by app environments") || !strings.Contains(r.stderr, name+"/production") {
		t.Fatalf("cluster delete exit=%d stderr=%s", r.exitCode, r.stderr)
	}
	runCLIExpectOK(t, "app", "env", "delete", name, "production", "--yes")
	waitForExit(t, 4, 10*time.Minute, "app", "env", "get", name, "production")
	runCLIExpectOK(t, "app", "delete", name, "--yes")
	waitForExit(t, 4, 5*time.Minute, "app", "get", name)
}

// cleanupApp removes the app and its environment without leaving either
// behind. App delete is refused while an environment is still deleting, so
// the environment must be gone (env get exits 4) before the app is deleted.
// Each step is bounded; a leftover app is reported as a test error.
func cleanupApp(t *testing.T, name, env string) {
	t.Helper()
	if runCLI(t, "app", "get", name).exitCode == 4 {
		return
	}
	_ = runCLI(t, "app", "env", "update", name, env, "--deletion-protection=false", "--wait=false")
	// Deletes run with --wait=false so these polls, not the CLI's 30m default
	// wait, bound each step inside the go test timeout.
	deadline := time.Now().Add(15 * time.Minute)
	for runCLI(t, "app", "env", "get", name, env).exitCode != 4 {
		_ = runCLI(t, "app", "env", "delete", name, env, "--yes", "--wait=false")
		if time.Now().After(deadline) {
			t.Errorf("cleanup: environment %s/%s is still present; app %s left behind", name, env, name)
			return
		}
		time.Sleep(5 * time.Second)
	}
	_ = runCLI(t, "app", "update", name, "--deletion-protection=false")
	deadline = time.Now().Add(5 * time.Minute)
	for {
		if r := runCLI(t, "app", "delete", name, "--yes", "--wait=false"); r.exitCode == 0 || r.exitCode == 4 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("cleanup: app %s could not be deleted; remove it with kupe app delete %s --yes", name, name)
			return
		}
		time.Sleep(5 * time.Second)
	}
}
