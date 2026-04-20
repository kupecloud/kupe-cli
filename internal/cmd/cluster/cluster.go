// Package cluster wires the "kupe cluster" subcommand tree: list, get,
// create, delete, update, kubeconfig (Phase 4), wait.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// NewCmd returns the parent cluster command with every v1 subcommand
// wired in. Phase 4's `kubeconfig` subcommand lands in this package later.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Kupe clusters",
		Long: `Create, list, inspect, update, and delete Kupe-managed virtual
Kubernetes clusters.

Long-running operations (create / delete / update) wait for the terminal
phase by default. Pass --wait=false to return as soon as the API accepts
the request.`,
	}

	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newGetCmd(f))
	cmd.AddCommand(newCreateCmd(f))
	cmd.AddCommand(newDeleteCmd(f))
	cmd.AddCommand(newUpdateCmd(f))
	cmd.AddCommand(newWaitCmd(f))
	cmd.AddCommand(newKubeconfigCmd(f))

	return cmd
}

// renderOne writes a single cluster in the requested output format.
func renderOne(out io.Writer, colorEnabled bool, format *printer.Format, c *client.Cluster) error {
	return printer.RenderOne(out, format, c, printer.ClusterDetailColumns(colorEnabled), func(c *client.Cluster) string { return c.Name })
}

// renderList writes a slice of clusters in the requested output format.
func renderList(out io.Writer, colorEnabled bool, format *printer.Format, cs []client.Cluster) error {
	return printer.RenderList(out, format, cs, printer.ClusterColumns(colorEnabled), func(c client.Cluster) string { return c.Name })
}

// phaseOf returns cluster's current phase or "" if unknown.
func phaseOf(c *client.Cluster) string {
	if c == nil || c.Status == nil {
		return ""
	}
	return c.Status.Phase
}

// waitForPhase polls api.GetCluster until the cluster's phase matches
// target. Degraded is treated as a terminal failure. Returns the last
// observed cluster for the success path.
func waitForPhase(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, label, target string, timeout time.Duration) (*client.Cluster, error) {
	var latest *client.Cluster
	poll := func(ctx context.Context) (string, bool, error) {
		c, _, err := api.GetCluster(ctx, name)
		if err != nil {
			return "", false, err
		}
		latest = c
		phase := phaseOf(c)
		if phase == client.PhaseDegraded {
			return phase, false, fmt.Errorf("cluster %q entered Degraded state", name)
		}
		if phase == target {
			return phase, true, nil
		}
		return phase, false, nil
	}
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:   label,
		Poll:    poll,
		Timeout: timeout,
	})
	return latest, err
}

// waitForUpdateConverged is the wait strategy for `cluster update --wait`.
// Unlike create/delete, where the cluster starts in a non-terminal phase
// and we can straightforwardly poll for the target, an update on a cluster
// that's already Running creates a race: the operator may not yet have
// observed the spec change, so the first GetCluster returns phase=Running
// from BEFORE the patch and we'd spuriously exit success before the
// rollout began.
//
// Fix: two phases.
//
//  1. Observation window — up to observeWindow, poll every second waiting
//     for phase to LEAVE Running (i.e., the operator has picked up the
//     change and started work). If the window expires without any phase
//     transition, treat the update as a no-op and return success with the
//     latest observed state.
//  2. Convergence — once we've seen a transition away from Running, fall
//     through to the normal waitForPhase(target=Running, timeout=timeout).
//
// This handles three cases correctly:
//
//   - Real update, operator quick: phase flips within the window → Phase 2
//     waits for Running → exit success after convergence.
//   - Real update, operator slow (>observeWindow): we exit with stale state.
//     Users who need stronger guarantees should `kupe cluster wait` after.
//   - No-op update (spec unchanged): phase never flips → window expires →
//     we exit success immediately without spurious timeout.
func waitForUpdateConverged(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, label string, observeWindow, timeout time.Duration) (*client.Cluster, error) {
	var latest *client.Cluster

	// Phase 1 — observation.
	obsCtx, cancel := context.WithTimeout(ctx, observeWindow)
	defer cancel()

	obsPoll := func(ctx context.Context) (string, bool, error) {
		c, _, err := api.GetCluster(ctx, name)
		if err != nil {
			return "", false, err
		}
		latest = c
		phase := phaseOf(c)
		// "Done" here means "we observed a non-Running phase" — the
		// operator has picked up the spec change.
		if phase != client.PhaseRunning && phase != "" {
			return phase, true, nil
		}
		return phase, false, nil
	}
	obsErr := ux.WaitFor(obsCtx, streams, ux.WaitForOpts{
		Label:    label + " (observing)",
		Poll:     obsPoll,
		Interval: 1 * time.Second,
		Max:      2 * time.Second,
	})

	if obsErr != nil {
		// DeadlineExceeded from the observation window = no transition
		// seen = no-op update. Return success.
		if errors.Is(obsErr, context.DeadlineExceeded) || errors.Is(obsErr, ux.ErrWaitTimeout) {
			return latest, nil
		}
		// Honour real errors (including parent ctx cancellation).
		if errors.Is(obsErr, context.Canceled) {
			return latest, obsErr
		}
		return latest, obsErr
	}

	// Phase 2 — convergence back to Running.
	return waitForPhase(ctx, streams, api, name, label, client.PhaseRunning, timeout)
}

// waitForGone polls until the cluster returns 404 (or Terminating flips to
// gone). Used by `cluster delete --wait`.
func waitForGone(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, label string, timeout time.Duration) error {
	poll := func(ctx context.Context) (string, bool, error) {
		c, _, err := api.GetCluster(ctx, name)
		if err != nil {
			if client.IsNotFound(err) {
				return "Deleted", true, nil
			}
			return "", false, err
		}
		return phaseOf(c), false, nil
	}
	return ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:   label,
		Poll:    poll,
		Timeout: timeout,
	})
}

// mapWaitErr translates ux.ErrWaitTimeout into cli.TimeoutError so exit
// codes work out (8 for timeout). Other errors pass through unchanged.
// Uses errors.Is so wrapped timeouts (e.g. from nested deadlines) are
// still recognised.
func mapWaitErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ux.ErrWaitTimeout) {
		return cli.TimeoutError(err.Error()).WithHint("use \"kupe cluster get <name>\" to check status, or \"kupe cluster wait\" to resume")
	}
	return err
}

// parsedFormat parses the user's -o flag, falling back to
// preferences.output from the config file when the flag is empty.
// Delegates to printer.MustParse which wraps errors as cli.MisuseError.
func parsedFormat(f *cli.Factory, raw string) (*printer.Format, error) {
	if raw == "" {
		raw = f.DefaultOutput()
	}
	return printer.MustParse(raw)
}
