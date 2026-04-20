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

	start := time.Now()
	got, err := waitForUpdateConverged(context.Background(), f.IOStreams, fake, "prod", "cluster prod", 10*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("waitForUpdateConverged: %v", err)
	}
	if got == nil || got.Status == nil || got.Status.Phase != client.PhaseRunning {
		t.Fatalf("final phase = %+v; want Running", got)
	}
	// Must have taken at least one poll tick — if we returned in sub-ms
	// we bypassed transition observation entirely.
	if elapsed < 100*time.Millisecond {
		t.Fatalf("returned in %v — likely skipped transition observation", elapsed)
	}
}

// TestUpdateWaitTimesOutWhenOperatorNeverReacts is the contract for the
// bug fix: if the cluster stays Running for the whole --wait-timeout
// (operator ignored the PATCH, or the PATCH was a late-detected no-op),
// the waiter returns ErrWaitTimeout instead of silently succeeding.
// The old waiter exited success after a 30s observation window; we now
// refuse to mask that as green and let the command exit 8.
func TestUpdateWaitTimesOutWhenOperatorNeverReacts(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:   "prod",
		Status: &client.ClusterStatus{Phase: client.PhaseRunning},
	}
	f := factoryWith(t, fake)

	_, err := waitForUpdateConverged(context.Background(), f.IOStreams, fake, "prod", "cluster prod", 500*time.Millisecond)
	if err == nil {
		t.Fatal("stuck-on-Running update returned success; expected ErrWaitTimeout")
	}
	if !errors.Is(err, ux.ErrWaitTimeout) {
		t.Fatalf("err = %v; want ux.ErrWaitTimeout", err)
	}
}

// TestUpdateNoOpSkipsPATCHAndReturns asserts that when every update flag
// already matches the cluster's current spec, the CLI does NOT call PATCH
// at all — no operator transition to wait for, no round trip, no risk of
// the waiter spuriously timing out.
func TestUpdateNoOpSkipsPATCHAndReturns(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:      "prod",
		Version:   "1.32",
		Resources: &client.ClusterResource{CPU: "4", Memory: "16Gi", Storage: "100Gi"},
		Status:    &client.ClusterStatus{Phase: client.PhaseRunning},
	}
	f := factoryWith(t, fake)

	// Re-requesting the exact same version must not trigger a PATCH.
	if err := runCmd(newUpdateCmd(f), "prod", "--version", "1.32", "--cpu", "4", "--wait=false"); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, call := range fake.Calls {
		if strings.HasPrefix(call, "UpdateCluster") {
			t.Fatalf("no-op update still hit the API: %v", fake.Calls)
		}
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
