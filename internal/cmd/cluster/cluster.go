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
		Long: `Create, list, inspect, update, and delete Kupe clusters.

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
// The cluster is expected to be Running at entry; a successful PATCH should
// cause the operator to transition phase away from Running (Upgrading /
// Provisioning / etc.) and then return it to Running once the rollout
// settles.
//
// The waiter requires BOTH transitions before declaring success:
//
//  1. Phase must leave Running — proof that the operator observed the
//     spec change and started acting on it.
//  2. Phase must return to Running — proof the rollout completed.
//
// If the operator never picks up the change within timeout, the call
// returns ErrWaitTimeout and the command surfaces exit 8 — NOT silent
// success. That "no transition observed = all good" shortcut was the
// source of a false-green bug: CI would move on before the rollout
// started, and the next step would see the pre-patch state.
//
// Callers MUST filter client-side no-ops (spec already matches) BEFORE
// invoking the waiter; see isNoOpUpdate. If a no-op reaches this code
// path, it will correctly time out — the operator has no transition to
// produce. Exposing that as failure is preferable to exposing it as
// success.
func waitForUpdateConverged(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, label string, timeout time.Duration) (*client.Cluster, error) {
	var (
		latest        *client.Cluster
		sawTransition bool
	)
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
		if !sawTransition {
			if phase != "" && phase != client.PhaseRunning {
				sawTransition = true
			}
			return phase, false, nil
		}
		if phase == client.PhaseRunning {
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

// waitForGone polls until the cluster returns 404 (or Terminating flips to
// gone). Used by `cluster delete --wait`. Reports "Deleting" while the
// cluster still exists (regardless of underlying phase — the API uses
// "Terminating" but "Deleting" is friendlier on the user-facing line),
// and "Deleted" when GetCluster 404s.
func waitForGone(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, label string, timeout time.Duration) error {
	poll := func(ctx context.Context) (string, bool, error) {
		_, _, err := api.GetCluster(ctx, name)
		if err != nil {
			if client.IsNotFound(err) {
				return "Deleted", true, nil
			}
			return "", false, err
		}
		return "Deleting", false, nil
	}
	return ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:    label,
		DoneVerb: "deleted",
		Poll:     poll,
		Timeout:  timeout,
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
