package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// --- Fix 1: displayName falls back to NAME ---------------------------------

// TestCreateDisplayNameFallsBackToName asserts that `kupe cluster create NAME`
// without --display-name sends NAME as the displayName. The server rejects
// empty displayName (kupe-api handler_cluster.go:414), so the CLI must
// supply the fallback.
func TestCreateDisplayNameFallsBackToName(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)

	err := runCmd(newCreateCmd(f), "prod", "--type", "shared", "--wait=false")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	c, ok := fake.Clusters["prod"]
	if !ok {
		t.Fatal("cluster not created")
	}
	if c.DisplayName != "prod" {
		t.Fatalf("displayName = %q; want fallback to NAME (%q)", c.DisplayName, "prod")
	}
}

func TestCreateDisplayNameRespectsFlag(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)

	err := runCmd(newCreateCmd(f), "prod", "--type", "shared", "--display-name", "Production Cluster", "--wait=false")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := fake.Clusters["prod"].DisplayName; got != "Production Cluster" {
		t.Fatalf("displayName = %q; want %q", got, "Production Cluster")
	}
}

// --- Fix 3: update wait observes operator convergence ---------------------

// TestUpdateWaitRequiresTransitionBeforeRunning simulates the race the agent
// flagged: the cluster is already Running when update is submitted, and the
// operator takes a few polls to flip phase to Upgrading. A naive waiter
// returns success on the first Running read — before any rollout happens.
// The convergence-aware waiter must observe a transition first.
func TestUpdateWaitRequiresTransitionBeforeRunning(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:    "prod",
		Type:    "shared",
		Version: "1.32",
		Status:  &client.ClusterStatus{Phase: client.PhaseRunning},
	}

	// Seed GetCluster to return Running, then Upgrading (operator picked
	// up the change), then Running (converged). The naive waiter would
	// have returned at entry 0 before any work started.
	fake.GetClusterSeq["prod"] = []*client.Cluster{
		{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseRunning}},
		{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseUpgrading}},
		{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseRunning}},
	}

	f := factoryWith(t, fake)

	// Use a tight observation window — the scripted sequence reaches
	// Upgrading by the 2nd poll so 5s is plenty.
	start := time.Now()
	got, err := waitForUpdateConverged(context.Background(), f.IOStreams, fake, "prod", "cluster prod", 5*time.Second, 10*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("waitForUpdateConverged: %v", err)
	}
	if got == nil || got.Status == nil || got.Status.Phase != client.PhaseRunning {
		t.Fatalf("final phase = %+v; want Running", got)
	}
	// Observation must have taken at least one tick (the seeded sequence
	// had to be drained). If we returned in sub-ms we bypassed observation.
	if elapsed < 100*time.Millisecond {
		t.Fatalf("returned in %v — likely skipped observation window", elapsed)
	}
}

// TestUpdateWaitNoOpReturnsWithinObservationWindow verifies that when a
// patch is a no-op (operator has nothing to do), the waiter does NOT
// hang for the full --wait-timeout — it returns success once the
// observation window expires without seeing a transition.
func TestUpdateWaitNoOpReturnsWithinObservationWindow(t *testing.T) {
	fake := clienttest.New()
	// Cluster is Running and stays Running forever (simulating a no-op).
	fake.Clusters["prod"] = &client.Cluster{
		Name:   "prod",
		Status: &client.ClusterStatus{Phase: client.PhaseRunning},
	}
	f := factoryWith(t, fake)

	start := time.Now()
	got, err := waitForUpdateConverged(context.Background(), f.IOStreams, fake, "prod", "cluster prod", 300*time.Millisecond, 30*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("no-op update waited with error: %v", err)
	}
	if got == nil {
		t.Fatal("returned nil cluster")
	}
	// Should return roughly at observation-window time, not at wait-timeout.
	if elapsed > 2*time.Second {
		t.Fatalf("no-op waited %v; expected near the observation window (~300ms)", elapsed)
	}
}

// --- Fix 5: update with no mutation flags exits 2 -------------------------

// TestUpdateWithNoFlagsExits2 asserts that `kupe cluster update NAME` with
// no --version/--cpu/--memory/--storage exits as a misuse (2), not a
// silent success.
func TestUpdateWithNoFlagsExits2(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:   "prod",
		Status: &client.ClusterStatus{Phase: client.PhaseRunning},
	}
	f := factoryWith(t, fake)

	err := runCmd(newUpdateCmd(f), "prod", "--wait=false")
	if err == nil {
		t.Fatal("expected misuse error, got nil")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
	if !strings.Contains(err.Error(), "at least one of") {
		t.Fatalf("error message should mention required flags: %v", err)
	}

	// And the fake API must not have been touched — no Update call made.
	for _, call := range fake.Calls {
		if strings.HasPrefix(call, "UpdateCluster") {
			t.Fatalf("no-op update still hit the API: %v", fake.Calls)
		}
	}
}

// --- Helper (separate name so it doesn't collide with cluster_test.go's) ---

func runCmd(cmd interface {
	SetContext(context.Context)
	SetArgs([]string)
	Execute() error
}, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

// Keep fmt import in case future assertions need it.
var _ = fmt.Sprintf
var _ = ux.ErrWaitTimeout
var _ = errors.Is
