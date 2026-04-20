package cluster

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/kubeconfig"
)

type kubeconfigOpts struct {
	merge          bool
	contextName    string
	userName       string
	clusterName    string
	exec           bool
	force          bool
	forceOverwrite bool
}

func newKubeconfigCmd(f *cli.Factory) *cobra.Command {
	opts := &kubeconfigOpts{}

	cmd := &cobra.Command{
		Use:   "kubeconfig NAME",
		Short: "Fetch or merge a cluster kubeconfig",
		Long: `Generate a kubectl-compatible kubeconfig for a cluster.

By default, prints the full kubeconfig YAML to stdout so you can pipe it to
a file. With --merge, merges into $KUBECONFIG (or ~/.kube/config) and
promotes the new context to current.

Two user-entry modes:

  Token mode (default) — embeds the current API token directly. Simple, but
    the emitted kubeconfig is a secret that expires with the API key.

  Exec mode (--exec)  — emits an exec-plugin stanza that shells back to
    "kupe auth get-token --context=..." whenever kubectl needs a token.
    Contains no secrets; safe to commit.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Print to stdout, redirect into a file
  kupe cluster kubeconfig prod > /tmp/kc

  # Merge into ~/.kube/config
  kupe cluster kubeconfig prod --merge

  # Exec-plugin mode — kubeconfig is token-free
  kupe cluster kubeconfig prod --merge --exec`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKubeconfig(cmd, f, args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.merge, "merge", false, "Merge into $KUBECONFIG or ~/.kube/config instead of printing")
	cmd.Flags().StringVar(&opts.contextName, "context-name", "", "Context name in the generated kubeconfig (default: kupe-<tenant>-<cluster>)")
	cmd.Flags().StringVar(&opts.userName, "user-name", "", "User entry name (default: same as context-name)")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster entry name (default: same as context-name)")
	cmd.Flags().BoolVar(&opts.exec, "exec", false, "Emit an exec-plugin kubeconfig that shells back to `kupe auth get-token`")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Overwrite colliding entries on --merge")
	cmd.Flags().BoolVar(&opts.forceOverwrite, "force-overwrite", false, "If the existing kubeconfig is corrupt, discard it and start fresh (data-loss operation — use with care)")
	return cmd
}

func runKubeconfig(cmd *cobra.Command, f *cli.Factory, name string, opts *kubeconfigOpts) error {
	api, err := f.Client()
	if err != nil {
		return err
	}
	kc, err := api.GetClusterKubeconfig(cmd.Context(), name)
	if err != nil {
		if client.IsUnavailable(err) {
			return cli.New(cli.ExitUnavailable, fmt.Sprintf("cluster %q is not yet ready", name)).
				WithHint("pair with \"kupe cluster wait " + name + " --for running\" or retry once it reaches Running")
		}
		return err
	}

	names := resolveNames(api.Tenant(), name, opts)
	built, err := buildKubeconfig(f, names, kc, opts)
	if err != nil {
		return err
	}

	if opts.merge {
		target, err := kubeconfig.TargetPath("")
		if err != nil {
			return cli.Wrap(cli.ExitGeneral, "resolving kubeconfig path", err)
		}
		mergeOpts := kubeconfig.MergeOptions{Force: opts.force, ForceOverwrite: opts.forceOverwrite}
		if err := kubeconfig.Merge(target, built, mergeOpts); err != nil {
			if isCollision(err) {
				return cli.ConflictError(err.Error()).WithHint("re-run with --force to overwrite")
			}
			if errors.Is(err, kubeconfig.ErrCorrupt) {
				return cli.ConflictError(err.Error()).
					WithHint("inspect " + target + " manually, back it up, then re-run with --force-overwrite")
			}
			return err
		}
		fmt.Fprintf(f.IOStreams.ErrOut,
			"Merged %q into %s.\n  use \"kubectl --context %s\" to target it.\n",
			names.Context, target, names.Context,
		)
		return nil
	}

	data, err := kubeconfig.Marshal(built)
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "rendering kubeconfig", err)
	}
	_, err = f.IOStreams.Out.Write(data)
	return err
}

// buildKubeconfig assembles the kubeconfig in the requested mode.
func buildKubeconfig(f *cli.Factory, names kubeconfig.Names, kc *client.ClusterKubeconfig, opts *kubeconfigOpts) (*clientcmdapi.Config, error) {
	if opts.exec {
		binary := execBinary()
		ctxName := ""
		if r, err := f.Resolved(); err == nil {
			ctxName = r.ContextName
		}
		return kubeconfig.BuildExecConfig(names, kc.Endpoint, kc.CertificateAuthority, binary, ctxName)
	}
	tok, err := f.Token()
	if err != nil {
		return nil, err
	}
	return kubeconfig.BuildTokenConfig(names, kc.Endpoint, kc.CertificateAuthority, tok)
}

// resolveNames honours explicit --*-name flags while filling the rest from
// the DefaultNames template. If the user sets only --context-name, mirror
// it onto user/cluster so the three stay aligned (most common intent).
func resolveNames(tenant, cluster string, opts *kubeconfigOpts) kubeconfig.Names {
	base := kubeconfig.DefaultNames(tenant, cluster)
	if opts.contextName != "" {
		base.Context = opts.contextName
		if opts.userName == "" {
			base.User = opts.contextName
		}
		if opts.clusterName == "" {
			base.Cluster = opts.contextName
		}
	}
	if opts.userName != "" {
		base.User = opts.userName
	}
	if opts.clusterName != "" {
		base.Cluster = opts.clusterName
	}
	return base
}

// execBinary returns the path to the kupe binary to invoke from exec-plugin
// kubeconfigs. Prefers an absolute path so the generated kubeconfig keeps
// working when the user's PATH changes (e.g., a shell without /usr/local/bin).
func execBinary() string {
	if p, err := os.Executable(); err == nil && p != "" {
		return p
	}
	return "kupe"
}

// isCollision reports whether err wraps kubeconfig.ErrCollision.
func isCollision(err error) bool {
	return errors.Is(err, kubeconfig.ErrCollision)
}
