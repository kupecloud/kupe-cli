package registry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// connectionNoun labels a cluster connection in progress output.
const connectionNoun = "cluster connection"

// maxPushNamespaces mirrors the platform's per-connection cap
// (maxNamespacesPerBinding), so a longer list is refused before a request.
const maxPushNamespaces = 10

// defaultPushSecretName is the push Secret's name when --secret-name is not
// given (the platform's default); pullSecretName is the platform's own and
// can never be used.
const (
	defaultPushSecretName = "kupe-registry-push" // #nosec G101 -- a Secret's name, not a credential
	pullSecretName        = "kupe-registry-pull" // #nosec G101 -- a Secret's name, not a credential
)

var (
	// dnsLabel is a cluster or namespace name: a DNS-1123 label.
	dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	// dnsSubdomain is a Secret name: a DNS-1123 subdomain.
	dnsSubdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
)

// validateCluster checks a CLUSTER argument: a DNS label without "--".
func validateCluster(cluster string) error {
	if len(cluster) > 63 || !dnsLabel.MatchString(cluster) || strings.Contains(cluster, "--") {
		return cli.MisuseError(fmt.Sprintf("invalid cluster name %q", cluster)).
			WithHint("cluster names are lower-case letters, digits and single '-', at most 63 characters; list them with:  kupe cluster list")
	}
	return nil
}

// validatePushNamespaces checks push namespaces before a request: DNS
// labels, none reserved, no repeats, at most maxPushNamespaces. kupe-api is
// authoritative; this only gives the message earlier.
func validatePushNamespaces(namespaces []string) error {
	seen := map[string]bool{}
	for _, ns := range namespaces {
		switch {
		case len(ns) > 63 || !dnsLabel.MatchString(ns):
			return cli.MisuseError(fmt.Sprintf("invalid namespace %q", ns)).
				WithHint("namespaces are lower-case letters, digits and '-', at most 63 characters")
		case strings.HasPrefix(ns, "kube-") || ns == "argocd":
			return cli.MisuseError(fmt.Sprintf("namespace %q is reserved for the platform and cannot be a push namespace", ns))
		case seen[ns]:
			return cli.MisuseError(fmt.Sprintf("namespace %q is listed twice", ns))
		}
		seen[ns] = true
	}
	if n := len(namespaces); n > maxPushNamespaces {
		return cli.MisuseError(fmt.Sprintf("a cluster connection can have at most %d push namespaces; this would have %d", maxPushNamespaces, n))
	}
	return nil
}

// validateSecretName checks --secret-name: a DNS-1123 subdomain, never the
// platform's pull Secret.
func validateSecretName(name string) error {
	switch {
	case name == "":
		return nil
	case name == pullSecretName:
		return cli.MisuseError(fmt.Sprintf("--secret-name cannot be %s: the platform uses that name", pullSecretName))
	case len(name) > 253 || !dnsSubdomain.MatchString(name):
		return cli.MisuseError(fmt.Sprintf("invalid --secret-name %q", name)).
			WithHint("use lower-case letters, digits, '-' and '.', e.g. " + defaultPushSecretName)
	}
	return nil
}

type connectOpts struct {
	pushNamespaces []string
	secretName     string
	wait           bool
	waitTimeout    time.Duration
	output         string
}

func newConnectCmd(f *cli.Factory) *cobra.Command {
	opts := &connectOpts{}
	cmd := &cobra.Command{
		Use:   "connect CLUSTER [--push-namespace NAMESPACE]...",
		Short: "Let a cluster's pods pull from the registry",
		Long: `Connect a cluster to the registry. Every pod in the cluster can then pull
from it without an imagePullSecret of its own; the pull credential stays on
the platform and never enters the cluster.

Pods that were already running keep their old settings: restart them to pull
from the registry.

--push-namespace (repeatable, up to 10) also gives a namespace a Secret that
can push to the registry, for CI that builds inside the cluster. That Secret
can pull from and push to every repository in your registry, including
replacing existing tags. Its name is
--secret-name (default kupe-registry-push) and cannot change later. A registry
has at most 20 push namespaces across all its connections.

The cluster must already exist in your tenant (see kupe cluster list). By
default the command waits until the connection is Ready.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Pull only
  kupe registry connect prod

  # Pull, and let the ci namespace push
  kupe registry connect build --push-namespace ci`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster := args[0]
			if err := validateCluster(cluster); err != nil {
				return err
			}
			if err := validatePushNamespaces(opts.pushNamespaces); err != nil {
				return err
			}
			if err := validateSecretName(opts.secretName); err != nil {
				return err
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			reg, _, err := api.Registries().Get(cmd.Context(), client.RegistryName)
			if err != nil {
				return translateRegistryErr(err)
			}
			b, _, err := api.RegistryBindings(client.RegistryName).Create(cmd.Context(), client.CreateRegistryBindingRequest{
				Name: cluster, Cluster: cluster, PushNamespaces: opts.pushNamespaces, SecretName: opts.secretName,
			})
			if err != nil {
				return translateConnectErr(err, cluster)
			}
			if opts.wait {
				final, werr := waitForConnection(cmd.Context(), f.IOStreams, api, cluster, opts.waitTimeout)
				if werr != nil {
					return connectionWaitErr(werr, cluster, final, "the connection was made")
				}
				b = final
			}
			if err := printer.RegistryBinding.One(f.IOStreams, format, b); err != nil {
				return err
			}
			printConnectNotes(f.IOStreams, b, endpointOf(reg), opts.wait)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&opts.pushNamespaces, "push-namespace", nil, "Namespace inside the cluster that gets a push Secret; repeat for each, up to 10")
	cmd.Flags().StringVar(&opts.secretName, "secret-name", "", "Name of the push Secret in each push namespace (default kupe-registry-push); cannot change later")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the connection to be Ready before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// endpointOf is the registry's endpoint, or the default host while the
// platform reports none.
func endpointOf(r *client.Registry) string {
	if r != nil && r.Status != nil && r.Status.Endpoint != "" {
		return r.Status.Endpoint
	}
	return client.DefaultRegistryHost
}

// printConnectNotes tells a person on stderr what a connection means for
// pods already running and, with push namespaces, what their Secret can do.
func printConnectNotes(io *cli.IOStreams, b *client.RegistryBinding, endpoint string, ready bool) {
	fmt.Fprintln(io.ErrOut)
	if ready {
		fmt.Fprintf(io.ErrOut, "Workloads in %s can now pull from %s. Pods that were running before this connection was made keep their old settings: restart them to pull from the registry, for example \"kubectl rollout restart deployment -n NAMESPACE NAME\".\n",
			b.Cluster, endpoint)
	} else {
		fmt.Fprintf(io.ErrOut, "Once the connection is Ready, workloads in %s can pull from %s. Pods that were running before then keep their old settings: restart them to pull from the registry, for example \"kubectl rollout restart deployment -n NAMESPACE NAME\".\n",
			b.Cluster, endpoint)
	}
	if len(b.PushNamespaces) > 0 {
		secret := b.SecretName
		if secret == "" {
			secret = defaultPushSecretName
		}
		fmt.Fprintf(io.ErrOut, "Each push namespace has a Secret named %s that can pull from and push to every repository in your registry, including replacing existing tags.\n", secret)
	}
}

// connectionPhase is the phase a wait reads. The platform's top-level phase
// can be stale right after a change, so it is Failed while a listed push
// namespace is Failed, and Ready only once the connection is Ready, every
// listed push namespace is reported Ready and no removed one is still being
// withdrawn; a Ready not borne out by the namespaces reads Pending.
func connectionPhase(b *client.RegistryBinding) string {
	if b.Status == nil {
		return ""
	}
	reported := map[string]string{}
	for _, ns := range b.Status.Namespaces {
		reported[ns.Name] = ns.Phase
	}
	settled := true
	for _, ns := range b.PushNamespaces {
		switch reported[ns] {
		case client.PhaseFailed:
			return client.PhaseFailed
		case client.PhaseReady:
		default:
			settled = false
		}
	}
	for name := range reported {
		if !slices.Contains(b.PushNamespaces, name) {
			settled = false
		}
	}
	if b.Status.Phase == client.PhaseReady && !settled {
		return client.PhasePending
	}
	return b.Status.Phase
}

// connectionGetter reads a connection for the waits, its errors translated.
func connectionGetter(api client.Interface, cluster string) cmdutil.PhaseGetter[*client.RegistryBinding] {
	return func(ctx context.Context) (*client.RegistryBinding, string, error) {
		b, _, err := api.RegistryBindings(client.RegistryName).Get(ctx, cluster)
		if err != nil {
			return nil, "", translateErr(err, cluster)
		}
		return b, connectionPhase(b), nil
	}
}

// waitForConnection waits until the connection of cluster is Ready.
func waitForConnection(ctx context.Context, io *cli.IOStreams, api client.Interface, cluster string, timeout time.Duration) (*client.RegistryBinding, error) {
	return cmdutil.WaitForPhase(ctx, io, connectionNoun, cluster, client.PhaseReady, connectionGetter(api, cluster), timeout)
}

// connectionWaitErr explains why waiting for a connection ended before it
// was Ready; accepted says what was accepted before the wait.
func connectionWaitErr(err error, cluster string, last *client.RegistryBinding, accepted string) error {
	check := "check status:  kupe registry connection get " + cluster
	var terminal *cmdutil.TerminalPhaseError
	switch {
	case errors.As(err, &terminal):
		msg := fmt.Sprintf("the connection of cluster %q failed", cluster)
		if last != nil && last.Status != nil && last.Status.Message != "" {
			msg += ": " + last.Status.Message
		}
		if last != nil && last.Status.Conflicted() {
			return cli.ConflictError(msg).WithHint(fmt.Sprintf(
				"a push namespace already has a Secret named %s that the platform did not create; rename or remove it, and the connection becomes Ready on its own\n%s",
				last.SecretName, check))
		}
		return cli.New(cli.ExitGeneral, msg).WithHint(check)
	case errors.Is(err, ux.ErrWaitTimeout):
		msg := fmt.Sprintf("the connection of cluster %q is not Ready yet", cluster)
		if last != nil && last.Status != nil && last.Status.Message != "" {
			msg += ": " + last.Status.Message
		}
		return cli.TimeoutError(msg).WithHint(accepted + " and the platform keeps trying; it becomes Ready on its own once that clears\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, "stopped waiting; "+accepted+" and is still being set up").WithHint(check)
	case cmdutil.IsTransientWaitErr(err):
		return cli.Wrap(cli.ExitUnavailable, "lost contact with kupe-api while waiting", err).WithHint(check)
	}
	return err
}

func newDisconnectCmd(f *cli.Factory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "disconnect CLUSTER",
		Short: "Stop a cluster pulling from the registry",
		Long: `Remove a cluster's connection to the registry: its pull credential and every
push Secret it delivered are withdrawn. Pods in the cluster that restart after
this cannot pull images from your registry.

The request is asynchronous. Prompts on a TTY; pass --yes to skip (required
in non-interactive environments).`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry disconnect staging --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster := args[0]
			io := f.IOStreams
			if !yes {
				fmt.Fprintf(io.ErrOut, "\nPods in %s that restart after this cannot pull images from your registry.\n\n", cluster)
			}
			if err := cli.ConfirmDelete(io, yes, connectionNoun, cluster); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.RegistryBindings(client.RegistryName).Delete(cmd.Context(), cluster); err != nil {
				return translateErr(err, cluster)
			}
			fmt.Fprintf(io.Out, "connection/%s delete requested\n", cluster)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}

func newConnectionCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connection",
		Aliases: []string{"connections"},
		Short:   "Inspect and change cluster connections",
		Long: `List and inspect the clusters connected to the registry, change their push
namespaces, and rotate their credentials. Connect a cluster with
"kupe registry connect" and remove it with "kupe registry disconnect".`,
	}
	cmd.AddCommand(newConnectionListCmd(f))
	cmd.AddCommand(newConnectionGetCmd(f))
	cmd.AddCommand(newConnectionUpdateCmd(f))
	cmd.AddCommand(newConnectionRotateCmd(f))
	return cmd
}

func newConnectionListCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the clusters connected to the registry",
		Args:  cobra.NoArgs,
		Example: `  kupe registry connection list
  kupe registry connection list -o wide`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.RegistryBindings(client.RegistryName).List(cmd.Context())
			if err != nil {
				return translateErr(err, "")
			}
			return printer.RegistryBinding.List(f.IOStreams, format, list)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newConnectionGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "get CLUSTER",
		Short:   "Show a cluster connection, with its usernames and push namespaces",
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry connection get prod`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, _, err := api.RegistryBindings(client.RegistryName).Get(cmd.Context(), args[0])
			if err != nil {
				return translateErr(err, args[0])
			}
			return printer.RegistryBinding.One(f.IOStreams, format, b)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

type connectionUpdateOpts struct {
	set, add, remove []string
	clear            bool
	wait             bool
	waitTimeout      time.Duration
	output           string
}

func newConnectionUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &connectionUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update CLUSTER",
		Short: "Change the push namespaces of a cluster connection",
		Long: `Change which namespaces of a connected cluster hold a push Secret.
--push-namespace (repeatable) replaces the whole list; --add-push-namespace and
--remove-push-namespace edit it; --clear-push-namespaces removes every push
Secret, leaving pull only. A connection has at most 10 push namespaces, and a
registry at most 20 across all its connections; reserved namespaces (kube-*,
argocd) are refused. The checks here give an early message; kupe-api decides.

The push Secret's name was fixed when the cluster was connected: to change it,
disconnect the cluster and connect it again.

The command reads the connection, edits the list and writes it back; if the
connection changed in between, it reads it again and retries once. By default
it then waits until every listed namespace is Ready and every removed one is
withdrawn.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe registry connection update build --add-push-namespace release
  kupe registry connection update build --remove-push-namespace ci
  kupe registry connection update build --push-namespace ci --push-namespace release
  kupe registry connection update build --clear-push-namespaces`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster := args[0]
			if err := opts.validate(); err != nil {
				return err
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			var refused error
			unchanged := false
			updated, err := api.RegistryBindings(client.RegistryName).UpdateRMW(cmd.Context(), cluster,
				func(current *client.RegistryBinding) *client.PatchRegistryBindingRequest {
					next := opts.apply(current.PushNamespaces)
					refused = validatePushNamespaces(next)
					unchanged = refused == nil && slices.Equal(next, current.PushNamespaces)
					if refused != nil || unchanged {
						return nil
					}
					return &client.PatchRegistryBindingRequest{PushNamespaces: &next}
				})
			if refused != nil {
				return refused
			}
			if err != nil {
				return translateErr(err, cluster)
			}
			if unchanged {
				fmt.Fprintf(f.IOStreams.ErrOut, "the connection of cluster %q is unchanged: its push namespaces are %s\n", cluster, pushNamespaceList(updated.PushNamespaces))
			}
			if opts.wait {
				final, werr := waitForConnection(cmd.Context(), f.IOStreams, api, cluster, opts.waitTimeout)
				if werr != nil {
					return connectionWaitErr(werr, cluster, final, "the change was accepted")
				}
				updated = final
			}
			return printer.RegistryBinding.One(f.IOStreams, format, updated)
		},
	}
	cmd.Flags().StringArrayVar(&opts.set, "push-namespace", nil, "Replace the push namespaces with these; repeat for each")
	cmd.Flags().StringArrayVar(&opts.add, "add-push-namespace", nil, "Push namespace to add; repeat for each")
	cmd.Flags().StringArrayVar(&opts.remove, "remove-push-namespace", nil, "Push namespace to remove; repeat for each")
	cmd.Flags().BoolVar(&opts.clear, "clear-push-namespaces", false, "Remove every push namespace, leaving pull only")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until every listed namespace is Ready and every removed one is withdrawn")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// validate checks the flag combination and the names given.
func (o *connectionUpdateOpts) validate() error {
	editing := len(o.add) > 0 || len(o.remove) > 0
	switch {
	case len(o.set) > 0 && (editing || o.clear), editing && o.clear:
		return cli.MisuseError("pass --push-namespace, --add-push-namespace/--remove-push-namespace, or --clear-push-namespaces, not a mix")
	case len(o.set) == 0 && !editing && !o.clear:
		return cli.MisuseError("nothing to update: pass --push-namespace, --add-push-namespace, --remove-push-namespace or --clear-push-namespaces")
	}
	for _, ns := range o.add {
		if slices.Contains(o.remove, ns) {
			return cli.MisuseError(fmt.Sprintf("namespace %s is both added and removed", ns))
		}
	}
	if err := validatePushNamespaces(o.set); err != nil {
		return err
	}
	return validatePushNamespaces(o.add)
}

// apply is the new push namespace list from current: the --push-namespace
// list, none, or current with the additions appended (those not listed yet,
// in order) and the removals taken out.
func (o *connectionUpdateOpts) apply(current []string) []string {
	switch {
	case o.clear:
		return []string{}
	case len(o.set) > 0:
		return slices.Clone(o.set)
	}
	next := slices.DeleteFunc(slices.Clone(current), func(ns string) bool { return slices.Contains(o.remove, ns) })
	for _, ns := range o.add {
		if !slices.Contains(next, ns) {
			next = append(next, ns)
		}
	}
	if next == nil {
		next = []string{}
	}
	return next
}

func pushNamespaceList(namespaces []string) string {
	if len(namespaces) == 0 {
		return "none (pull only)"
	}
	return strings.Join(namespaces, ", ")
}

// rotation is what connection rotate reports with -o: the request kupe-api
// acknowledged and, once a wait has seen it applied, the token applied.
type rotation struct {
	Cluster     string `json:"cluster" yaml:"cluster"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
	AppliedAt   string `json:"appliedAt,omitempty" yaml:"appliedAt,omitempty"`
	Pending     bool   `json:"pending" yaml:"pending"`
}

type rotateOpts struct {
	yes         bool
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newConnectionRotateCmd(f *cli.Factory) *cobra.Command {
	opts := &rotateOpts{}
	cmd := &cobra.Command{
		Use:   "rotate CLUSTER",
		Short: "Replace a cluster connection's pull and push credentials",
		Long: `Replace the pull credential and the push Secrets of a cluster connection, and
by default wait until the new ones are in place. The old credentials stop
working once the rotation is applied; anything that copied a push Secret's
value elsewhere must read it again.

Prompts on a TTY; pass --yes to skip (required in non-interactive
environments). With --wait=false the command returns once the rotation is
requested; it is applied once status.rotation.appliedAt equals the
requestedAt printed (-o yaml shows both).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe registry connection rotate prod
  kupe registry connection rotate prod --yes --wait=false -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster := args[0]
			io := f.IOStreams
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("This will replace the registry credentials of cluster %q; the current ones stop working once it is done.", cluster)
			if err := cli.ConfirmAction(io, opts.yes, "rotate credentials", prompt); err != nil {
				return err
			}
			ack, err := api.RotateRegistryBinding(cmd.Context(), client.RegistryName, cluster)
			if err != nil {
				return translateErr(err, cluster)
			}
			result := rotation{Cluster: cluster, RequestedAt: ack.RequestedAt, Pending: true}
			waited := false
			if opts.wait && ack.RequestedAt != "" {
				observable, werr := waitForRotation(cmd.Context(), io, api, &result, opts.waitTimeout)
				switch {
				case werr != nil:
					return rotationWaitErr(werr, cluster)
				case !observable:
					fmt.Fprintln(io.ErrOut, "kupe-api does not report rotation progress, so the command cannot wait for it; the rotation was requested.")
				default:
					waited = true
				}
			}
			if format.Kind != printer.Table && format.Kind != printer.Wide {
				return printer.RenderOne(io.Out, format, &result, nil, func(r *rotation) string { return r.Cluster })
			}
			if waited {
				fmt.Fprintf(io.Out, "connection/%s credentials rotated\n", cluster)
				return nil
			}
			fmt.Fprintf(io.Out, "connection/%s credentials rotation requested\n", cluster)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the new credentials are in place before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 10*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: table | json | yaml | go-template=...")
	return cmd
}

// waitForRotation polls the connection until the rotation requested with
// result.RequestedAt is applied (appliedAt equals it and nothing is
// pending). A rotation requested meanwhile replaces this one: the wait
// follows it, says so once, and result is updated. observable is false when
// the platform reports no rotation state.
func waitForRotation(ctx context.Context, io *cli.IOStreams, api client.Interface, result *rotation, timeout time.Duration) (observable bool, err error) {
	get := connectionGetter(api, result.Cluster)
	b, _, err := get(ctx)
	switch {
	case err != nil && !cmdutil.IsTransientWaitErr(err):
		return true, err
	case err == nil && (b.Status == nil || b.Status.Rotation == nil):
		return false, nil
	}
	poll := cmdutil.PollPhase(get, func(b *client.RegistryBinding, _ string) (bool, error) {
		if b.Status == nil || b.Status.Rotation == nil {
			return false, nil
		}
		rot := b.Status.Rotation
		if rot.RequestedAt != "" && rot.RequestedAt != result.RequestedAt {
			fmt.Fprintf(io.ErrOut, "A newer rotation (%s) replaced this one; waiting for it instead.\n", rot.RequestedAt)
			result.RequestedAt = rot.RequestedAt
		}
		if rot.AppliedAt == result.RequestedAt && !rot.Pending {
			result.AppliedAt, result.Pending = rot.AppliedAt, false
			return true, nil
		}
		return false, nil
	})
	err = ux.WaitFor(ctx, io, ux.WaitForOpts{
		Label:         connectionNoun + " " + result.Cluster,
		DoneVerb:      "credentials rotated",
		PhaseOverride: "Rotating",
		Poll:          poll,
		Timeout:       timeout,
	})
	return true, err
}

// rotationWaitErr explains why waiting for a rotation ended before it was
// applied; the rotation was requested in every case.
func rotationWaitErr(err error, cluster string) error {
	check := fmt.Sprintf("check status:  kupe registry connection get %s -o yaml   (status.rotation: applied once appliedAt equals requestedAt)", cluster)
	switch {
	case errors.Is(err, ux.ErrWaitTimeout):
		return cli.TimeoutError(fmt.Sprintf("the credential rotation of cluster %q is not applied yet", cluster)).
			WithHint("the rotation was requested and the platform keeps applying it\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting; the credential rotation of cluster %q was requested and continues", cluster)).
			WithHint(check)
	case cmdutil.IsTransientWaitErr(err):
		return cli.Wrap(cli.ExitUnavailable, "lost contact with kupe-api while waiting", err).WithHint(check)
	}
	return err
}
