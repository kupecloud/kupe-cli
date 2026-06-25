// Package cluster wires the "kupe cluster" subcommand tree: list, get,
// create, delete, update, kubeconfig (Phase 4), wait.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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
		Poll:    tolerateTransient(poll),
		Timeout: timeout,
	})
	return latest, err
}

// waitForUpdateConverged is the wait strategy for `cluster update --wait`.
//
// Convergence is declared when the operator has reconciled at the
// post-PATCH generation AND the cluster is back in Running phase:
//
//	status.observedGeneration >= postPatchGeneration  &&  phase == Running
//
// observedGeneration is the canonical K8s "I've processed this spec" signal
// — it increments alongside metadata.generation each time the operator
// completes a reconcile. Using it here unblocks two failure modes the older
// transition-detection logic suffered from:
//
//  1. In-place updates (CPU/memory/storage limit bumps) never leave Running
//     because the operator just patches the ResourceQuota — there is no
//     rollout to observe. The old "must see phase != Running" gate hung
//     forever in this case.
//  2. Version upgrades DO transition Running → Upgrading → Running, but
//     under observedGeneration the same code path handles both: the
//     operator only stamps the new generation once the upgrade is complete,
//     so we wait for the right moment without phase-string heuristics.
//
// Degraded is still a hard fail. postPatchGen of 0 disables the generation
// gate (older servers that don't surface generation), in which case the
// waiter degrades to "phase == Running" — same as a get-loop, which is
// what the old code would have done before its transition gate was added.
func waitForUpdateConverged(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, label string, postPatchGen int64, timeout time.Duration) (*client.Cluster, error) {
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
		if phase != client.PhaseRunning {
			return phase, false, nil
		}
		if postPatchGen > 0 && observedGen(c) < postPatchGen {
			// Spec change accepted by the API, but the operator hasn't
			// yet stamped a reconcile at this generation. Keep waiting.
			return phase, false, nil
		}
		return phase, true, nil
	}
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:         label,
		PhaseOverride: "Updating",
		Poll:          tolerateTransient(poll),
		Timeout:       timeout,
	})
	return latest, err
}

// transientWaitGrace is how long a wait loop tolerates *consecutive* transient
// API errors (5xx, 429, network/transport) before giving up. Covers a rolling
// kupe-api deploy, an LB blip, or a brief 502 without aborting a long
// `--wait`. A first success resets the window. See KC-7.
const transientWaitGrace = 90 * time.Second

// isTransientWaitErr reports whether err is a transient failure a wait loop
// should ride out rather than treat as terminal: any non-APIError (network /
// transport / context-independent I/O) or a 5xx / 429 from kupe-api. A 404,
// 401, 403, or 400 is authoritative and must stay terminal — callers handle
// 404 themselves (e.g. delete-wait treats it as success).
func isTransientWaitErr(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation/deadline is handled by the waiter itself; never
	// swallow it here.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if client.IsUnavailable(err) || client.IsRateLimited(err) || client.IsServerError(err) {
		return true
	}
	// A typed 4xx (401/403/404/400/409/412) is authoritative — terminal.
	if client.IsUnauthorized(err) || client.IsForbidden(err) || client.IsNotFound(err) ||
		client.IsValidation(err) || client.IsConflict(err) || client.IsPreconditionFailed(err) {
		return false
	}
	// Anything left that isn't a typed APIError is a transport/network error
	// (the client already exhausted its short internal retry) — transient.
	return !client.IsAPIError(err)
}

// tolerateTransient wraps a poll function so consecutive transient errors are
// swallowed (the loop keeps polling, surfacing the last known phase) until
// transientWaitGrace elapses, after which the error becomes terminal. A
// successful poll resets the window. Terminal errors pass through immediately.
func tolerateTransient(poll ux.PollFunc) ux.PollFunc {
	var firstFailure time.Time
	lastPhase := ""
	return func(ctx context.Context) (string, bool, error) {
		phase, done, err := poll(ctx)
		if err == nil {
			firstFailure = time.Time{}
			lastPhase = phase
			return phase, done, nil
		}
		if !isTransientWaitErr(err) {
			return phase, done, err
		}
		now := time.Now()
		if firstFailure.IsZero() {
			firstFailure = now
		}
		if now.Sub(firstFailure) > transientWaitGrace {
			return phase, false, err
		}
		// Keep waiting: report the last known phase, no error.
		return lastPhase, false, nil
	}
}

// observedGen returns status.observedGeneration or 0 if the status block is
// missing. Centralised so the waiter doesn't sprinkle nil-checks.
func observedGen(c *client.Cluster) int64 {
	if c == nil || c.Status == nil {
		return 0
	}
	return c.Status.ObservedGeneration
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
		Poll:     tolerateTransient(poll),
		Timeout:  timeout,
	})
}

// translateClusterErr rewrites server-side validation messages from kupe-api /
// the operator's admission webhook into CLI-shaped guidance. The webhook and
// API leak K8s field paths ("spec.resources.cpu") that mean nothing to a CLI
// user — translate to flag names ("--cpu-limit") and pin a Hint where there's a
// next step the user can take.
//
// Only patterns we can match with confidence get rewritten; anything else
// falls through unchanged so we don't paper over a real bug with a friendly-
// looking but wrong message. Kept in one place so create/update/delete share
// the same vocabulary.
func translateClusterErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "spec.resources.cpu is required"):
		return cli.MisuseError("--cpu-limit is required").
			WithHint("example: --cpu-limit 2 (use a number of vCPUs, or a millicore value like 500m)")
	case strings.Contains(msg, "spec.resources.memory is required"):
		return cli.MisuseError("--memory-limit is required").
			WithHint("example: --memory-limit 8Gi (the unit suffix Gi/Mi is required)")
	case strings.Contains(msg, "spec.resources.storage is required"):
		return cli.MisuseError("--storage-limit is required").
			WithHint("example: --storage-limit 50Gi (the unit suffix Gi/Mi is required)")
	case strings.Contains(msg, "memory must include a unit suffix"):
		return cli.MisuseError("--memory-limit must include a unit suffix").
			WithHint("example: --memory-limit 8Gi or --memory-limit 8192Mi (plain numbers are ambiguous)")
	case strings.Contains(msg, "storage must include a unit suffix"):
		return cli.MisuseError("--storage-limit must include a unit suffix").
			WithHint("example: --storage-limit 50Gi or --storage-limit 50G")
	case strings.Contains(msg, "cluster name is required"):
		return cli.MisuseError("cluster NAME is required")
	case strings.Contains(msg, "cluster name must"):
		// Strip the leading "cluster name " so we don't say it twice when
		// we prefix with the supplied NAME below.
		detail := strings.TrimPrefix(msg, "cluster name ")
		// In some paths the admission framework wraps the message with
		// extra context; collapse to the descriptive tail.
		if i := strings.Index(detail, "cluster name "); i >= 0 {
			detail = detail[i+len("cluster name "):]
		}
		return cli.MisuseError("invalid cluster name: " + detail)
	case strings.Contains(msg, "unsupported Kubernetes version"):
		return cli.MisuseError(extractWebhookMessage(msg)).
			WithHint("run \"kupe plan list\" to see supported versions")
	case strings.Contains(msg, "maximum cluster limit reached"):
		return cli.New(cli.ExitConflict, extractWebhookMessage(msg)).
			WithHint("delete a cluster (\"kupe cluster delete <name>\") or upgrade your plan")
	case strings.Contains(msg, "Cluster creation is temporarily paused"):
		return cli.New(cli.ExitUnavailable, extractWebhookMessage(msg)).
			WithHint("retry in a few minutes; status updates are posted at https://status.kupe.cloud")
	case strings.Contains(msg, "is being deleted; cannot create new ManagedClusters"):
		return cli.New(cli.ExitConflict, "your tenant is being deleted; cannot create new clusters")
	case strings.Contains(msg, "spec.tenantRef is immutable"):
		return cli.MisuseError("a cluster's tenant cannot be changed after creation")
	case strings.Contains(msg, "tenantRef.namespace must match"):
		return cli.MisuseError("internal: tenantRef namespace mismatch — please file an issue")
	case strings.Contains(msg, "check field values and constraints"):
		// kupe-api's fallback for K8s validation errors that didn't come
		// from a webhook (or whose webhook prefix wasn't matched). The
		// most common cause for cluster create/update is bad --cpu-limit /
		// --memory-limit / --storage-limit values; point users there rather than
		// re-printing the opaque server message.
		return cli.MisuseError("invalid cluster spec").
			WithHint("check --cpu-limit (e.g. 2), --memory-limit (e.g. 8Gi), --storage-limit (e.g. 50Gi), and --version values")
	}
	return err
}

// extractWebhookMessage returns the substring that's the actual webhook
// message — strips a leading status-class word our APIError formatter may
// have prepended (none today, but defensive against future format tweaks).
func extractWebhookMessage(msg string) string {
	for _, prefix := range []string{"permission denied: ", "forbidden: "} {
		if strings.HasPrefix(msg, prefix) {
			msg = strings.TrimPrefix(msg, prefix)
			break
		}
	}
	if i := strings.Index(msg, " (request-id: "); i > 0 {
		msg = msg[:i]
	}
	return msg
}

// mapWaitErr translates errors out of a long-running wait into typed CLI
// errors with actionable hints. The two interesting cases:
//
//   - timeout: still in flight server-side, exit 8
//   - cancellation (Ctrl-C): the API call already succeeded; the resource
//     is being created/updated/deleted on Kupe Cloud regardless of whether
//     we kept watching. Tell the user that explicitly so they don't think
//     the operation aborted, exit 130
//
// `verb` is "create" / "update" / "delete" — drives the wording so the
// message accurately reflects what's still in progress.
func mapWaitErr(err error, name, verb string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ux.ErrWaitTimeout) {
		return cli.TimeoutError(err.Error()).WithHint(fmt.Sprintf(
			"check status:  kupe cluster get %s\nresume wait:   kupe cluster wait %s", name, name))
	}
	if errors.Is(err, context.Canceled) {
		switch verb {
		case "delete":
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; cluster %q is still being deleted on Kupe Cloud", name)).
				WithHint(fmt.Sprintf(
					"check status:  kupe cluster get %s\nresume wait:   kupe cluster wait %s --for=Deleted", name, name))
		case "update":
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; cluster %q is still being updated on Kupe Cloud", name)).
				WithHint(fmt.Sprintf(
					"check status:  kupe cluster get %s\nresume wait:   kupe cluster wait %s", name, name))
		case "wait":
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; cluster %q may still be transitioning on Kupe Cloud", name)).
				WithHint(fmt.Sprintf("check status:  kupe cluster get %s", name))
		default:
			return cli.New(cli.ExitInterrupted,
				fmt.Sprintf("stopped waiting; cluster %q is still being created on Kupe Cloud", name)).
				WithHint(strings.Join([]string{
					fmt.Sprintf("check status:  kupe cluster get %s", name),
					fmt.Sprintf("resume wait:   kupe cluster wait %s", name),
					fmt.Sprintf("abandon:       kupe cluster delete %s", name),
				}, "\n"))
		}
	}
	// A transient API error that outlived the grace window (KC-7): the wait
	// failed mid-flight, but the operation is almost certainly still
	// progressing server-side. Attach the same check-status / resume-wait
	// guidance the timeout path gets so CI doesn't see a bare 502/503.
	if client.IsUnavailable(err) || client.IsRateLimited(err) || client.IsServerError(err) || !client.IsAPIError(err) {
		hint := fmt.Sprintf("check status:  kupe cluster get %s\nresume wait:   kupe cluster wait %s", name, name)
		if verb == "delete" {
			hint = fmt.Sprintf("check status:  kupe cluster get %s\nresume wait:   kupe cluster wait %s --for=Deleted", name, name)
		}
		return cli.Wrap(cli.ExitUnavailable, "lost contact with kupe-api while waiting", err).WithHint(hint)
	}
	return err
}
