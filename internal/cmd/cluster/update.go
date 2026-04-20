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

			fmt, err := parsedFormat(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}

			mutator := buildMutator(opts)

			// The RunE precondition above guarantees at least one mutation
			// field is set, so buildMutator is guaranteed to return a
			// non-nil patch — simplifying the three branches.
			var updated *client.Cluster
			switch {
			case opts.force:
				// No If-Match; pass empty etag.
				current, _, gerr := api.GetCluster(cmd.Context(), name)
				if gerr != nil {
					return gerr
				}
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, "", *mutator(current))
				if uerr != nil {
					return uerr
				}
				updated = u
			case opts.ifMatch != "":
				current, _, gerr := api.GetCluster(cmd.Context(), name)
				if gerr != nil {
					return gerr
				}
				u, _, uerr := api.UpdateCluster(cmd.Context(), name, opts.ifMatch, *mutator(current))
				if uerr != nil {
					return uerr
				}
				updated = u
			default:
				u, uerr := api.UpdateClusterRMW(cmd.Context(), name, mutator)
				if uerr != nil {
					return uerr
				}
				updated = u
			}

			if !opts.wait {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmt, updated)
			}
			// updates need convergence detection: phase may already be Running
			// from before the patch, so waitForPhase would return instantly.
			final, werr := waitForUpdateConverged(
				cmd.Context(), f.IOStreams, api, name, "cluster "+name,
				30*time.Second, opts.waitTimeout,
			)
			if werr != nil {
				return mapWaitErr(werr)
			}
			if final == nil {
				final = updated
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmt, final)
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

// buildMutator turns the update flags into a ClusterMutator. RunE's
// precondition rejects all-empty-flags as MisuseError before this runs,
// so the returned mutator always produces a non-nil patch.
func buildMutator(opts *updateOpts) client.ClusterMutator {
	return func(_ *client.Cluster) *client.PatchClusterRequest {
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
}
