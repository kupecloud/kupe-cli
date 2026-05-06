package cluster

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type createOpts struct {
	typ         string
	displayName string
	version     string
	cpu         string
	memory      string
	storage     string
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{wait: true, waitTimeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new cluster",
		Long: `Submit a new ManagedCluster to kupe-api and (by default) wait for it to
reach the Running phase. Pass --wait=false to return immediately with just
the resource name — useful in scripts that spin up many clusters in parallel.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Default shared cluster, wait for Running
  kupe cluster create prod

  # Dedicated, specific version and resources, fire-and-forget
  kupe cluster create ci-$GITHUB_SHA \
    --type shared --version 1.32 --cpu 2 --memory 8Gi --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			fmt, err := printer.Resolve(f, opts.output)
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
				Name:        name,
				DisplayName: displayName,
				Type:        opts.typ,
				Version:     opts.version,
			}
			if r := resourceRequest(opts.cpu, opts.memory, opts.storage); r != nil {
				req.Resources = r
			}

			created, _, err := api.CreateCluster(cmd.Context(), req)
			if err != nil {
				return err
			}

			if !opts.wait {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmt, created)
			}

			final, werr := waitForPhase(cmd.Context(), f.IOStreams, api, name, "cluster "+name, client.PhaseRunning, opts.waitTimeout)
			if werr != nil {
				return mapWaitErr(werr)
			}
			if final == nil {
				final = created
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, fmt, final)
		},
	}

	cmd.Flags().StringVar(&opts.typ, "type", "shared", "Cluster type: shared or dedicated")
	cmd.Flags().StringVar(&opts.displayName, "display-name", "", "Human-readable display name (defaults to NAME)")
	cmd.Flags().StringVar(&opts.version, "version", "", "Kubernetes minor version (e.g. 1.32). Defaults to the platform default if unset.")
	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "CPU limit for the cluster (e.g. 2, 500m)")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "Memory limit for the cluster (e.g. 8Gi, 512Mi)")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "Control-plane storage size (e.g. 100Gi)")
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

// ensure fmt import stays in case future format strings are introduced.
var _ = fmt.Sprintf
