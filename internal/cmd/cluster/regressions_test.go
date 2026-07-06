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

	err := runCmd(newCreateCmd(f), "prod", "--cpu-limit", "2", "--memory-limit", "8Gi", "--storage-limit", "50Gi",
		"--wait=false")
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

	err := runCmd(newCreateCmd(f), "prod", "--display-name", "Production Cluster",
		"--cpu-limit", "2", "--memory-limit", "8Gi", "--storage-limit", "50Gi",
		"--wait=false")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := fake.Clusters["prod"].DisplayName; got != "Production Cluster" {
		t.Fatalf("displayName = %q; want %q", got, "Production Cluster")
	}
}

// --- Fix 3: update wait observes operator convergence ---------------------

// TestUpdateWaitBlocksUntilObservedGenerationCatchesUp covers the in-place
// update case (CPU/memory/storage bump): phase never leaves Running, so the
// only convergence signal is status.observedGeneration catching up to the
// post-PATCH metadata.generation. A naive Running-only waiter would return
// at entry 0 before the operator has reconciled.
func TestUpdateWaitBlocksUntilObservedGenerationCatchesUp(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:    "prod",
		Type:    "shared",
		Version: "1.32",
		Status:  &client.ClusterStatus{Phase: client.PhaseRunning},
	}

	// Operator stamps observedGeneration on its 3rd reconcile after the
	// PATCH. Earlier reads are Running but stale — must NOT be accepted.
	fake.GetClusterSeq["prod"] = []*client.Cluster{
		{Name: "prod", Generation: 5, Status: &client.ClusterStatus{Phase: client.PhaseRunning, ObservedGeneration: 4}},
		{Name: "prod", Generation: 5, Status: &client.ClusterStatus{Phase: client.PhaseRunning, ObservedGeneration: 4}},
		{Name: "prod", Generation: 5, Status: &client.ClusterStatus{Phase: client.PhaseRunning, ObservedGeneration: 5}},
	}

	f := factoryWith(t, fake)

	got, err := waitForUpdateConverged(context.Background(), f.IOStreams, fake, "prod", "cluster prod", 5, 10*time.Second)
	if err != nil {
		t.Fatalf("waitForUpdateConverged: %v", err)
	}
	if got == nil || got.Status == nil || got.Status.ObservedGeneration < 5 {
		t.Fatalf("final cluster = %+v; want observedGeneration>=5", got)
	}
}

// TestUpdateWaitTimesOutWhenOperatorNeverReacts is the contract for the
// bug fix: if the operator never stamps a reconcile at the post-PATCH
// generation, the waiter returns ErrWaitTimeout instead of silently
// succeeding. Catches a wedged operator or a PATCH that landed but never
// got reconciled.
func TestUpdateWaitTimesOutWhenOperatorNeverReacts(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:       "prod",
		Generation: 7,
		Status:     &client.ClusterStatus{Phase: client.PhaseRunning, ObservedGeneration: 6}, // stale
	}
	f := factoryWith(t, fake)

	_, err := waitForUpdateConverged(context.Background(), f.IOStreams, fake, "prod", "cluster prod", 7, 500*time.Millisecond)
	if err == nil {
		t.Fatal("operator never converged; expected ErrWaitTimeout")
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
	if err := runCmd(newUpdateCmd(f), "prod", "--version", "1.32", "--cpu-limit", "4", "--wait=false"); err != nil {
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
// no --version/--cpu-limit/--memory-limit/--storage-limit exits as a misuse
// (2), not a silent success.
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

// --- review-fable-2 HIGH-1: --wait-timeout exit-code contract --------------

// TestMapWaitErrExitCodes pins the documented exit codes for the two ways a
// wait ends early: timeout exhaustion → 8, user interrupt (Ctrl-C) → 130.
// Regression guard for the TTY spinner misclassifying a fired --wait-timeout
// as context.Canceled (exit 130 with the "stopped waiting" wording).
func TestMapWaitErrExitCodes(t *testing.T) {
	timeoutErr := mapWaitErr(ux.ErrWaitTimeout, "prod", "create")
	if code := cli.ExitCode(timeoutErr); code != cli.ExitTimeout {
		t.Fatalf("wait timeout exit = %d; want %d", code, cli.ExitTimeout)
	}
	if strings.Contains(timeoutErr.Error(), "stopped waiting") {
		t.Fatalf("timeout must not carry the Ctrl-C wording: %v", timeoutErr)
	}

	cancelErr := mapWaitErr(context.Canceled, "prod", "create")
	if code := cli.ExitCode(cancelErr); code != cli.ExitInterrupted {
		t.Fatalf("interrupt exit = %d; want %d", code, cli.ExitInterrupted)
	}
	if !strings.Contains(cancelErr.Error(), "stopped waiting") {
		t.Fatalf("interrupt should explain the operation continues: %v", cancelErr)
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
