package cluster

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

type updateOpts struct {
	version     string
	cpu         string
	memory      string
	storage     string
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
		Long: `Update a mutable field on a cluster. Exactly one of --version / --cpu /
--memory / --storage should be provided; combining several mutations in a
single request is supported but unusual.

Uses ETag-based optimistic locking (GET → mutate → PATCH with If-Match) by
default and retries once on a 412 mismatch. Pass --force to skip the check
— rarely the right answer.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe cluster update prod --version 1.33
  kupe cluster update prod --cpu 4 --memory 16Gi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// kupe-api rejects empty PATCH bodies (handler_cluster.go:244
			// "at least one field required"); catch that client-side so
			// misconfigured CI pipelines don't silently report success on
			// a no-op invocation.
			if opts.version == "" && opts.cpu == "" && opts.memory == "" && opts.storage == "" {
				return cli.MisuseError("nothing to update: pass at least one of --version, --cpu, --memory, --storage")
			}

			fmtp, err := parsedFormat(f, opts.output)
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

			patch := buildPatch(opts)

			var updated *client.Cluster
			switch {
			case opts.force:
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, "", *patch)
				if uerr != nil {
					return uerr
				}
				updated = u
			case opts.ifMatch != "":
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, opts.ifMatch, *patch)
				if uerr != nil {
					return uerr
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
					return uerr
				}
				updated = u
			}

			if !opts.wait {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, updated)
			}
			// Real spec change was sent; wait the full timeout for the
			// operator to transition phase away from Running and back again.
			// No silent-success shortcut for "no transition observed" — that
			// was the source of the false-green bug.
			final, werr := waitForUpdateConverged(
				cmd.Context(), f.IOStreams, api, name, "cluster "+name, opts.waitTimeout,
			)
			if werr != nil {
				return mapWaitErr(werr)
			}
			if final == nil {
				final = updated
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, final)
		},
	}

	cmd.Flags().StringVar(&opts.version, "version", "", "New Kubernetes version")
	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "New CPU limit")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "New memory limit")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "New storage size")
	cmd.Flags().StringVar(&opts.ifMatch, "if-match", "", "Require a specific ETag; exits 5 on mismatch")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Skip ETag optimistic-locking (advanced; risk of lost writes)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the cluster to return to Running before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up after this long; exits 8 on timeout")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: table|json|yaml|go-template=...")
	return cmd
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
