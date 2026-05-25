package cluster

import (
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type createOpts struct {
	typ              string
	displayName      string
	version          string
	cpu              string
	memory           string
	storage          string
	highAvailability bool
	wait             bool
	waitTimeout      time.Duration
	output           string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{wait: true, waitTimeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new cluster",
		Long: `Submit a new ManagedCluster to kupe-api and (by default) wait for it to
reach the Running phase. Pass --wait=false to return immediately with just
the resource name — useful in scripts that spin up many clusters in parallel.

--cpu-limit, --memory-limit, and --storage-limit are required. They cap the
resources your workloads on this cluster can consume; usage counts against
your tenant's pool. Run "kupe plan list" to see the pool your plan grants.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Smallest viable cluster, wait for Running
  kupe cluster create prod --cpu-limit 2 --memory-limit 8Gi --storage-limit 50Gi

  # Specific version, fire-and-forget
  kupe cluster create ci-$GITHUB_SHA \
    --type shared --version 1.32 \
    --cpu-limit 2 --memory-limit 8Gi --storage-limit 50Gi \
    --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Pre-flight client-side validation. Catches the common new-user
			// missteps (no resources, wrong unit format) before we round-
			// trip to the API and turn a webhook denial into a friendly
			// flag-shaped error. Server-side validation still runs — this
			// is a faster, more specific UX layer on top of it.
			if err := validateCreateOpts(opts); err != nil {
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

			// displayName is required server-side (kupe-api handler_cluster.go:414).
			// Fall back to NAME when the flag is empty — the flag help text
			// promises this, and sending an empty string would 400.
			displayName := opts.displayName
			if displayName == "" {
				displayName = name
			}
			req := client.CreateClusterRequest{
				Name:             name,
				DisplayName:      displayName,
				Type:             opts.typ,
				Version:          opts.version,
				HighAvailability: opts.highAvailability,
			}
			if r := resourceRequest(opts.cpu, opts.memory, opts.storage); r != nil {
				req.Resources = r
			}

			created, _, err := api.CreateCluster(cmd.Context(), req)
			if err != nil {
				return translateClusterErr(err)
			}

			if !opts.wait {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, created)
			}

			final, werr := waitForPhase(cmd.Context(), f.IOStreams, api, name, "cluster "+name, client.PhaseRunning, opts.waitTimeout)
			if werr != nil {
				return mapWaitErr(werr, name, "create")
			}
			if final == nil {
				final = created
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmtp, final)
		},
	}

	cmd.Flags().StringVar(&opts.typ, "type", "shared", "Cluster type: shared or dedicated")
	cmd.Flags().StringVar(&opts.displayName, "display-name", "", "Human-readable display name (defaults to NAME)")
	cmd.Flags().StringVar(&opts.version, "version", "", "Kubernetes minor version (e.g. 1.32). Defaults to the platform default if unset.")
	cmd.Flags().StringVar(&opts.cpu, "cpu-limit", "", "CPU limit for the cluster (e.g. 2, 500m)")
	cmd.Flags().StringVar(&opts.memory, "memory-limit", "", "Memory limit for the cluster (e.g. 8Gi, 512Mi)")
	cmd.Flags().StringVar(&opts.storage, "storage-limit", "", "Storage limit for the cluster (e.g. 50Gi)")
	cmd.Flags().BoolVar(&opts.highAvailability, "high-availability", false, "Provision a 3-replica HA control plane. Adds an hourly charge (see kupe plan for the rate).")
	// --ha is a short alias people will reach for in shell prompts. Hidden so
	// it doesn't crowd --help; documented in the --high-availability flag help.
	cmd.Flags().BoolVar(&opts.highAvailability, "ha", false, "Alias for --high-availability")
	_ = cmd.Flags().MarkHidden("ha")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the cluster to reach Running before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// resourceRequest returns a ClusterResource pointer if any of cpu/memory/
// storage were set, otherwise nil so the API receives a null field and
// applies the plan default.
func resourceRequest(cpu, memory, storage string) *client.ClusterResource {
	if cpu == "" && memory == "" && storage == "" {
		return nil
	}
	return &client.ClusterResource{CPU: cpu, Memory: memory, Storage: storage}
}

// cpuFormat permits an integer count of vCPUs ("2"), a millicore value
// ("500m"), or a fractional vCPU count ("1.5"). Mirrors what the operator
// will accept once it parses the value with k8s resource.Quantity, but we
// don't pull in that dependency just for client-side hinting.
var cpuFormat = regexp.MustCompile(`^\d+(\.\d+)?m?$`)

// memoryFormat / storageFormat require an explicit unit suffix. The webhook
// rejects bare numbers as ambiguous (8 bytes vs 8 GiB) — we replicate that
// rule here so the user gets the precise hint without round-tripping.
var memoryFormat = regexp.MustCompile(`^\d+(\.\d+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)$`)

// validateCreateOpts runs the same checks the operator's admission webhook
// runs, so the user sees flag-shaped errors instead of K8s field paths. The
// webhook is still authoritative — this is purely a UX shortcut.
func validateCreateOpts(opts *createOpts) error {
	if opts.cpu == "" {
		return cli.MisuseError("--cpu-limit is required").
			WithHint("example: --cpu-limit 2 (vCPUs, or millicores like 500m)")
	}
	if opts.memory == "" {
		return cli.MisuseError("--memory-limit is required").
			WithHint("example: --memory-limit 8Gi (the unit suffix Gi/Mi is required)")
	}
	// --storage-limit is required client-side even though the webhook
	// doesn't enforce it: when the operator sees empty storage it sets
	// the namespace ResourceQuota's requests.storage to 0, and the
	// vcluster's 5Gi data PVC then fails to provision. The user observes
	// this as a "Provisioning forever" cluster with no obvious error in
	// the ManagedCluster status. Catching it here turns silent failure
	// into a loud "you forgot --storage-limit" before the API call.
	if opts.storage == "" {
		return cli.MisuseError("--storage-limit is required").
			WithHint("example: --storage-limit 50Gi (the unit suffix Gi/Mi is required)")
	}
	if !cpuFormat.MatchString(opts.cpu) {
		return cli.MisuseError("--cpu-limit has an invalid format: " + opts.cpu).
			WithHint("use a number of vCPUs (e.g. 2, 1.5) or millicores (e.g. 500m)")
	}
	if !memoryFormat.MatchString(opts.memory) {
		return cli.MisuseError("--memory-limit must include a unit suffix; got: " + opts.memory).
			WithHint("example: --memory-limit 8Gi or --memory-limit 8192Mi (plain numbers are ambiguous)")
	}
	if !memoryFormat.MatchString(opts.storage) {
		return cli.MisuseError("--storage-limit must include a unit suffix; got: " + opts.storage).
			WithHint("example: --storage-limit 50Gi or --storage-limit 50G")
	}
	return nil
}
