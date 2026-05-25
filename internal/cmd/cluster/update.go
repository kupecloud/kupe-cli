package cluster

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type updateOpts struct {
	version     string
	cpu         string
	memory      string
	storage     string
	enableHA    bool
	disableHA   bool
	yes         bool
	ifMatch     string
	force       bool
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{wait: true, waitTimeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update a cluster's version or resources",
		Long: `Update a mutable field on a cluster. Exactly one of --version,
--cpu-limit, --memory-limit, or --storage-limit should be provided; combining
several mutations in a single request is supported but unusual.

By default the CLI checks the cluster hasn't changed since you last
read it (read-modify-write with a server-side version check) and retries
once on a concurrent-update collision. Pass --force to skip the check —
rarely the right answer outside of disaster recovery.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe cluster update prod --version 1.33
  kupe cluster update prod --cpu-limit 4 --memory-limit 16Gi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// kupe-api rejects empty PATCH bodies (handler_cluster.go:244
			// "at least one field required"); catch that client-side so
			// misconfigured CI pipelines don't silently report success on
			// a no-op invocation.
			if opts.version == "" && opts.cpu == "" && opts.memory == "" && opts.storage == "" && !opts.enableHA && !opts.disableHA {
				return cli.MisuseError("nothing to update: pass at least one of --version, --cpu-limit, --memory-limit, --storage-limit, --enable-ha, --disable-ha")
			}
			if opts.enableHA && opts.disableHA {
				return cli.MisuseError("--enable-ha and --disable-ha are mutually exclusive")
			}
			if err := validateUpdateOpts(opts); err != nil {
				return err
			}

			fmtp, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}

			// Pre-flight GET serves two purposes:
			//   1. Let us detect a no-op client-side (every flag already
			//      matches current spec) and skip the PATCH entirely — so
			//      the wait loop isn't asked to observe changes that will
			//      never happen.
			//   2. Supply the optimistic-locking etag for the default
			//      branch without a second round-trip via RMW.
			current, currentEtag, err := api.GetCluster(cmd.Context(), name)
			if err != nil {
				return err
			}
			if isNoOpUpdate(current, opts) {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, current)
			}

			// HA enable on an existing single-replica cluster triggers an
			// in-place kine→etcd migration with API downtime. Print the
			// canonical warning (mirrors HA_ENABLE_MIGRATION from the
			// operator webhook) and require explicit confirmation so a
			// stray --enable-ha never causes a surprise outage. --yes
			// bypasses for CI; non-TTY without --yes errors out so a
			// scripted invocation can't hang on stdin.
			if opts.enableHA && !current.HighAvailability {
				msg := fmt.Sprintf("Enabling HA on %q migrates kine→etcd in place.\n"+
					"Expect ~10 minutes of API downtime during the migration window.\n"+
					"HA billing accrues from the moment 3/3 replicas are ready — not before.\n\n"+
					"Continue?", name)
				if err := cli.ConfirmYesNo(f.IOStreams, opts.yes, msg); err != nil {
					return err
				}
			}

			patch := buildPatch(opts)

			var updated *client.Cluster
			switch {
			case opts.force:
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, "", *patch)
				if uerr != nil {
					return translateClusterErr(uerr)
				}
				updated = u
			case opts.ifMatch != "":
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, opts.ifMatch, *patch)
				if uerr != nil {
					return translateClusterErr(uerr)
				}
				updated = u
			default:
				// Optimistic PATCH with the etag from the pre-flight GET.
				// On 412 (another writer landed in between) fall back to
				// UpdateClusterRMW for one automatic GET+PATCH retry.
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, currentEtag, *patch)
				if client.IsPreconditionFailed(uerr) {
					u, uerr = api.UpdateClusterRMW(cmd.Context(), name, func(_ *client.Cluster) *client.PatchClusterRequest {
						return patch
					})
				}
				if uerr != nil {
					return translateClusterErr(uerr)
				}
				updated = u
			}

			if !opts.wait {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, updated)
			}
			// Wait for the operator to reconcile at the post-PATCH
			// generation. PATCH responses carry the bumped metadata.generation,
			// so we don't need a follow-up GET to capture it.
			final, werr := waitForUpdateConverged(
				cmd.Context(), f.IOStreams, api, name, "cluster "+name, updated.Generation, opts.waitTimeout,
			)
			if werr != nil {
				return mapWaitErr(werr, name, "update")
			}
			if final == nil {
				final = updated
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, final)
		},
	}

	cmd.Flags().StringVar(&opts.version, "version", "", "New Kubernetes minor version (e.g. 1.32). Run \"kupe plan list\" to see what's offered.")
	cmd.Flags().StringVar(&opts.cpu, "cpu-limit", "", "New CPU limit (e.g. 4, 500m)")
	cmd.Flags().StringVar(&opts.memory, "memory-limit", "", "New memory limit (e.g. 16Gi, 512Mi)")
	cmd.Flags().StringVar(&opts.storage, "storage-limit", "", "New storage limit (e.g. 100Gi)")
	cmd.Flags().BoolVar(&opts.enableHA, "enable-ha", false, "Enable HA on this cluster. Triggers a ~10-minute in-place kine→etcd migration with API downtime.")
	cmd.Flags().BoolVar(&opts.disableHA, "disable-ha", false, "Disable HA on this cluster. Not supported in v1 — operator will reject with HA_DISABLE_UNSUPPORTED.")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation (required in non-interactive environments for --enable-ha)")
	cmd.Flags().StringVar(&opts.ifMatch, "if-match", "", "Only update if the cluster's resourceVersion still matches this value; aborts otherwise (advanced)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Skip the resourceVersion check; may overwrite a concurrent update from another client (advanced)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the cluster to return to Running before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// validateUpdateOpts mirrors the format checks in validateCreateOpts. Each
// flag is optional on update — but if supplied, must be well-formed.
func validateUpdateOpts(opts *updateOpts) error {
	if opts.cpu != "" && !cpuFormat.MatchString(opts.cpu) {
		return cli.MisuseError("--cpu-limit has an invalid format: " + opts.cpu).
			WithHint("use a number of vCPUs (e.g. 2, 1.5) or millicores (e.g. 500m)")
	}
	if opts.memory != "" && !memoryFormat.MatchString(opts.memory) {
		return cli.MisuseError("--memory-limit must include a unit suffix; got: " + opts.memory).
			WithHint("example: --memory-limit 8Gi or --memory-limit 8192Mi (plain numbers are ambiguous)")
	}
	if opts.storage != "" && !memoryFormat.MatchString(opts.storage) {
		return cli.MisuseError("--storage-limit must include a unit suffix; got: " + opts.storage).
			WithHint("example: --storage-limit 50Gi or --storage-limit 50G")
	}
	return nil
}

// buildPatch turns the update flags into a PatchClusterRequest. RunE's
// precondition rejects all-empty-flags as MisuseError before this runs,
// so the returned patch always has at least one field populated.
func buildPatch(opts *updateOpts) *client.PatchClusterRequest {
	patch := &client.PatchClusterRequest{}
	if opts.version != "" {
		v := opts.version
		patch.Version = &v
	}
	if r := resourceRequest(opts.cpu, opts.memory, opts.storage); r != nil {
		patch.Resources = r
	}
	if opts.enableHA {
		t := true
		patch.HighAvailability = &t
	}
	if opts.disableHA {
		// We send the value through to the server even though we know it'll
		// reject — the operator's webhook owns the canonical error code
		// (HA_DISABLE_UNSUPPORTED) and we want the same string surfaced
		// here as in console/TF/kubectl direct.
		f := false
		patch.HighAvailability = &f
	}
	return patch
}

// isNoOpUpdate returns true when every flag the user passed already matches
// the cluster's current spec. String equality only — "4" and "4000m" compare
// unequal even though the operator would treat them the same. A user who
// passes a differently-formatted quantity will get a real PATCH and pay
// the full --wait-timeout if the operator decides nothing needs to change;
// that's the price of not pulling in the k8s quantity parser here.
func isNoOpUpdate(current *client.Cluster, opts *updateOpts) bool {
	if current == nil {
		return false
	}
	if opts.version != "" && opts.version != current.Version {
		return false
	}
	if opts.enableHA && !current.HighAvailability {
		return false
	}
	// --disable-ha is never a no-op: even on a non-HA cluster the operator
	// rejects it explicitly. Send it through so the user sees the canonical
	// HA_DISABLE_UNSUPPORTED rejection (or, on an HA cluster, the same).
	if opts.disableHA {
		return false
	}
	if current.Resources == nil {
		// Caller specified a resource override but the cluster has no
		// resource block yet — PATCH would create it, not a no-op.
		return opts.cpu == "" && opts.memory == "" && opts.storage == ""
	}
	res := current.Resources
	if opts.cpu != "" && opts.cpu != res.CPU {
		return false
	}
	if opts.memory != "" && opts.memory != res.Memory {
		return false
	}
	if opts.storage != "" && opts.storage != res.Storage {
		return false
	}
	return true
}
