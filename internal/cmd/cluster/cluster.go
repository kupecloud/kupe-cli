// Package cluster wires the "kupe cluster" subcommand tree: list, get,
// create, delete, update, kubeconfig (Phase 4), wait.
package cluster

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
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

// phaseOf returns cluster's current phase or "" if unknown.
func phaseOf(c *client.Cluster) string {
	if c == nil || c.Status == nil {
		return ""
	}
	return c.Status.Phase
}

// phaseGetter adapts GetCluster to the shared wait helpers.
func phaseGetter(api client.Interface, name string) cmdutil.PhaseGetter[*client.Cluster] {
	return func(ctx context.Context) (*client.Cluster, string, error) {
		c, _, err := api.Clusters().Get(ctx, name)
		return c, phaseOf(c), err
	}
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
	poll := cmdutil.PollPhase(phaseGetter(api, name), func(c *client.Cluster, phase string) (bool, error) {
		latest = c
		if phase == client.PhaseDegraded {
			return false, &cmdutil.TerminalPhaseError{Noun: "cluster", Name: name, Phase: phase}
		}
		if phase != client.PhaseRunning {
			return false, nil
		}
		// Spec change accepted by the API, but the operator hasn't yet
		// stamped a reconcile at this generation: keep waiting.
		return postPatchGen <= 0 || observedGen(c) >= postPatchGen, nil
	})
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:         label,
		PhaseOverride: "Updating",
		Poll:          poll,
		Timeout:       timeout,
	})
	return latest, err
}

// observedGen returns status.observedGeneration or 0 if the status block is
// missing. Centralised so the waiter doesn't sprinkle nil-checks.
func observedGen(c *client.Cluster) int64 {
	if c == nil || c.Status == nil {
		return 0
	}
	return c.Status.ObservedGeneration
}

// translateClusterErr rewrites server-side validation messages from kupe-api /
// the operator's admission webhook into CLI-shaped guidance. The webhook and
// API leak K8s field paths ("spec.resources.cpu") that mean nothing to a CLI
// user — translate to flag names ("--cpu-limit") and pin a Hint where there's a
// next step the user can take. The refusals every service shares (creation
// paused, terminating, the tenant gates, the plan limit, a 503) are
// cmdutil.TranslateCommon's; name is the cluster the command acted on.
//
// Only patterns we can match with confidence get rewritten; anything else
// falls through unchanged so we don't paper over a real bug with a friendly-
// looking but wrong message. Kept in one place so create/update/delete share
// the same vocabulary.
func translateClusterErr(err error, name string) error {
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
	case strings.Contains(msg, "spec.tenantRef is immutable"):
		return cli.MisuseError("a cluster's tenant cannot be changed after creation")
	case strings.Contains(msg, "tenantRef.namespace must match"):
		return cli.MisuseError("internal: tenantRef namespace mismatch — please file an issue")
	case client.IsValidation(err) && strings.HasPrefix(msg, clusterSpecRejectedPrefix):
		// The platform refused the spec. The API's reason is the error
		// text; the hint names the flags that set the validated fields.
		return cli.WrapAPI(cli.ExitMisuse, "cluster spec rejected", err).
			WithHint("check --cpu-limit (e.g. 2), --memory-limit (e.g. 8Gi), --storage-limit (e.g. 50Gi), and --version values")
	}
	return cmdutil.TranslateCommon("cluster", name, err)
}

// clusterSpecRejectedPrefix starts kupe-api's message for a cluster spec that
// failed validation (`invalid cluster "NAME": ...`). The quote keeps name
// rejections (`invalid cluster name "NAME": ...`) out of the spec hint.
const clusterSpecRejectedPrefix = "invalid cluster \""

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
