package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// maxNameLength is the longest resource name the API accepts.
const maxNameLength = 63

// maxNamespacesPerBinding mirrors the binding CRD's maxItems on
// spec.namespaces, so a longer list is refused before a request.
const maxNamespacesPerBinding = 10

func newBindingCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "binding",
		Short: "Bind a database into cluster namespaces",
		Long: `Manage database bindings. A binding grants one role of a database to up to
10 namespaces of one cluster: each namespace receives a Service named after
the database and a credentials Secret, and gains a network path to the
database. Other namespaces see neither.

Roles: owner can change the schema; readwrite can read and write data;
readonly can read data. "binding update" adds or removes namespaces; a
binding's cluster, role and Secret name are fixed: to change one, delete the
binding and add another.`,
	}

	cmd.AddCommand(newBindingAddCmd(f))
	cmd.AddCommand(newBindingListCmd(f))
	cmd.AddCommand(newBindingGetCmd(f))
	cmd.AddCommand(newBindingUpdateCmd(f))
	cmd.AddCommand(newBindingDeleteCmd(f))
	return cmd
}

type bindingAddOpts struct {
	cluster     string
	namespaces  []string
	role        string
	secretName  string
	name        string
	wait        bool
	waitTimeout time.Duration
	output      string
}

// bindingNoun labels a binding in progress output.
const bindingNoun = "database binding"

func newBindingAddCmd(f *cli.Factory) *cobra.Command {
	opts := &bindingAddOpts{}

	cmd := &cobra.Command{
		Use:   "add DATABASE --cluster CLUSTER --namespace NAMESPACE... --role owner|readwrite|readonly",
		Short: "Grant a database role to namespaces of a cluster",
		Long: `Bind a database into namespaces of one cluster. Pass --namespace once for
each namespace, up to 10. Applications in each namespace connect to the host
named after the database and read their credentials from the Secret (default
name: DATABASE-ROLE), which has the same name in every namespace.

By default the command waits until the binding is Ready in every namespace.
It is Pending while the database or the cluster does not exist yet or is not
running; the platform keeps trying, so the cluster does not have to exist
yet. It is Failed when a namespace already has a Service named after the
database, or a Secret of the binding's Secret name, that the platform did
not create: the command exits at once and says which names clash in which
namespace. Pass --wait=false to return as soon as the request is accepted.

The binding name defaults to DATABASE-CLUSTER-ROLE; pass --name if that is
longer than 63 characters. To add a namespace to a binding later, use
"kupe database binding update".`,
		Args: cobra.ExactArgs(1),
		Example: `  # The migration Job runs with the owner role, the API and its worker with the readwrite role
  kupe database binding add orders-db --cluster prod --namespace orders-migrate --role owner
  kupe db binding add orders-db --cluster prod --namespace orders-api --namespace orders-worker --role readwrite

  # Reporting reads with the readonly role
  kupe db binding add orders-db --cluster prod --namespace orders-reports --role readonly

  # Bind into a cluster that is still being created, without waiting
  kupe db binding add orders-db --cluster staging --namespace orders-api --role readwrite --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database := args[0]
			if opts.cluster == "" || len(opts.namespaces) == 0 {
				return cli.MisuseError("--cluster and --namespace are required")
			}
			if n := len(opts.namespaces); n > maxNamespacesPerBinding {
				return cli.MisuseError(fmt.Sprintf("a binding can list at most %d namespaces; %d given", maxNamespacesPerBinding, n)).
					WithHint("bind the others with a second binding of the same role (pass --name)")
			}
			if err := validateRole(opts.role); err != nil {
				return err
			}
			name := opts.name
			if name == "" {
				name = fmt.Sprintf("%s-%s-%s", database, opts.cluster, opts.role)
				if len(name) > maxNameLength {
					return cli.MisuseError(fmt.Sprintf("default binding name %q is longer than %d characters", name, maxNameLength)).
						WithHint("pass a shorter one with --name")
				}
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, err := api.CreateDatabaseBinding(cmd.Context(), database, client.CreateDatabaseBindingRequest{
				Name:       name,
				Cluster:    opts.cluster,
				Namespaces: opts.namespaces,
				Role:       opts.role,
				SecretName: opts.secretName,
			})
			if err != nil {
				return translateBindingAddErr(err, database, name, opts)
			}
			if !opts.wait {
				return printer.DatabaseBinding.One(f.IOStreams, format, b)
			}

			final, werr := cmdutil.WaitForPhase(cmd.Context(), f.IOStreams, bindingNoun, name, client.PhaseReady,
				bindingPhaseGetter(api, database, name), opts.waitTimeout)
			if werr != nil {
				return bindingWaitErr(werr, database, name, final, bindingCreated)
			}
			return printer.DatabaseBinding.One(f.IOStreams, format, final)
		},
	}

	cmd.Flags().StringVar(&opts.cluster, "cluster", "", "Cluster to bind into (required)")
	cmd.Flags().StringArrayVar(&opts.namespaces, "namespace", nil, "Namespace inside the cluster that receives the Service and Secret; repeat for each namespace, up to 10 (required)")
	cmd.Flags().StringVar(&opts.role, "role", "", "Role granted: owner (schema changes), readwrite (read and write data) or readonly (read data) (required)")
	cmd.Flags().StringVar(&opts.secretName, "secret-name", "", "Name of the credentials Secret in every namespace (default DATABASE-ROLE)")
	cmd.Flags().StringVar(&opts.name, "name", "", "Binding name (default DATABASE-CLUSTER-ROLE)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the binding to become Ready in every namespace before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func bindingPhaseGetter(api client.Interface, database, name string) cmdutil.PhaseGetter[*client.DatabaseBinding] {
	return func(ctx context.Context) (*client.DatabaseBinding, string, error) {
		b, err := api.GetDatabaseBinding(ctx, database, name)
		if err != nil {
			return nil, "", translateErr(err, database)
		}
		return b, bindingPhase(b), nil
	}
}

// bindingPhase is the phase a wait reads. The platform's top-level phase
// alone can be stale right after a change, so:
//   - Failed while a listed namespace is Failed (a name conflict there);
//   - Ready only once the binding is Ready, every listed namespace is
//     reported Ready and no removed namespace is still being withdrawn;
//   - otherwise Pending in place of a Ready or Failed not borne out by the
//     listed namespaces, else the platform's phase.
//
// Before the platform reports namespaces, a Failed binding is Failed and a
// Ready one Pending.
func bindingPhase(b *client.DatabaseBinding) string {
	if b.Status == nil {
		return ""
	}
	phase := b.Status.Phase
	if len(b.Status.Namespaces) == 0 && phase == client.PhaseFailed {
		return client.PhaseFailed
	}
	settled := len(b.Status.Namespaces) > 0
	for _, ns := range b.NamespaceStates() {
		if ns.Listed && ns.Reported && ns.Status.Phase == client.PhaseFailed {
			return client.PhaseFailed
		}
		if !ns.Listed || !ns.Reported || ns.Status.Phase != client.PhaseReady {
			settled = false
		}
	}
	switch {
	case phase == client.PhaseReady && settled:
		return client.PhaseReady
	case phase == client.PhaseReady || phase == client.PhaseFailed:
		return client.PhasePending
	}
	return phase
}

// translateBindingAddErr adds what a defaulted name clashing with an
// existing binding means: most often a binding of the same role into the
// same cluster that the namespaces can be added to.
func translateBindingAddErr(err error, database, name string, opts *bindingAddOpts) error {
	if opts.name != "" || !client.IsConflict(err) || client.ErrorCode(err) != "" {
		return translateErr(err, database)
	}
	add := ""
	for _, ns := range opts.namespaces {
		add += " --add-namespace " + ns
	}
	return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("a binding named %q already exists", name), err).WithHint(
		fmt.Sprintf("to add the namespaces to it:  kupe database binding update %s %s%s\n", database, name, add) +
			"or pass --name to add a second binding of the same role")
}

// bindingChange says what was accepted before a binding wait, for its
// error messages.
type bindingChange struct {
	accepted string // "the binding was created"
	stopped  string // "binding %q was created and is still being set up"
}

var (
	bindingCreated = bindingChange{
		accepted: "the binding was created",
		stopped:  "stopped waiting; binding %q was created and is still being set up",
	}
	bindingUpdated = bindingChange{
		accepted: "the change was accepted",
		stopped:  "stopped waiting; the change to binding %q was accepted and is still being applied",
	}
)

// bindingWaitErr explains why waiting for a binding ended without it being
// Ready in every listed namespace. last is the binding as last seen, nil if
// never seen. The request was accepted before the wait in every case.
func bindingWaitErr(err error, database, name string, last *client.DatabaseBinding, change bindingChange) error {
	var status client.DatabaseBindingStatus
	phase := ""
	if last != nil && last.Status != nil {
		status, phase = *last.Status, bindingPhase(last)
	}
	check := fmt.Sprintf("check status:  kupe database binding get %s %s", database, name)
	var terminal *cmdutil.TerminalPhaseError
	switch {
	case errors.As(err, &terminal):
		// Only a seen Failed binding ends the wait this way, so last is set.
		return bindingFailedErr(last, check)
	case errors.Is(err, ux.ErrWaitTimeout):
		msg := fmt.Sprintf("binding %q is not Ready yet", name)
		if phase != "" {
			msg = fmt.Sprintf("binding %q is still %s", name, phase)
		}
		// A Ready binding's message describes the listed namespaces only.
		if status.Message != "" && status.Phase != client.PhaseReady {
			msg += ": " + status.Message
		}
		if removing := removingNamespaces(last); len(removing) > 0 {
			msg += "; still removing " + quotedNamespaces(removing)
		}
		return cli.TimeoutError(msg).WithHint(change.accepted + " and the platform keeps trying; it becomes Ready on its own once that clears\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf(change.stopped, name)).
			WithHint(check)
	case cmdutil.IsTransientWaitErr(err):
		return cli.Wrap(cli.ExitUnavailable, "lost contact with kupe-api while waiting", err).WithHint(check)
	}
	return err
}

// bindingFailedErr reports a Failed binding with the platform's message. A
// name conflict exits like any other conflict, with what to change.
func bindingFailedErr(b *client.DatabaseBinding, check string) error {
	msg := fmt.Sprintf("binding %q failed", b.Name)
	if b.Status.Message != "" {
		msg += ": " + b.Status.Message
	}
	if !b.Status.Conflicted() {
		return cli.New(cli.ExitGeneral, msg).WithHint(check)
	}
	var hint []string
	for _, ns := range conflictedNamespaces(b) {
		hint = append(hint, fmt.Sprintf("namespace %s of cluster %s already has a Service %q or a Secret %q that the platform did not create",
			ns, b.Cluster, b.Database, b.EffectiveSecretName()))
	}
	hint = append(hint,
		"rename or remove that object, and the binding becomes Ready on its own",
		fmt.Sprintf("or, if the Secret is the clash, choose another Secret name:  kupe database binding delete %s %s --yes", b.Database, b.Name),
		"  then add the binding again with --secret-name",
	)
	if failed := conflictedNamespaces(b); len(failed) < len(b.Namespaces) {
		remove := ""
		for _, ns := range failed {
			remove += " --remove-namespace " + ns
		}
		hint = append(hint, fmt.Sprintf("or stop binding into it:  kupe database binding update %s %s%s", b.Database, b.Name, remove))
	}
	return cli.ConflictError(msg).WithHint(strings.Join(append(hint, check), "\n"))
}

// removingNamespaces are the namespaces b no longer lists whose Service and
// Secret the platform is still withdrawing.
func removingNamespaces(b *client.DatabaseBinding) []string {
	if b == nil {
		return nil
	}
	var out []string
	for _, ns := range b.NamespaceStates() {
		if !ns.Listed {
			out = append(out, ns.Name)
		}
	}
	return out
}

// quotedNamespaces names namespaces as the platform's messages do:
// namespace "a", or namespaces "a", "b".
func quotedNamespaces(namespaces []string) string {
	quoted := make([]string, len(namespaces))
	for i, ns := range namespaces {
		quoted[i] = strconv.Quote(ns)
	}
	if len(quoted) == 1 {
		return "namespace " + quoted[0]
	}
	return "namespaces " + strings.Join(quoted, ", ")
}

// conflictedNamespaces are the listed namespaces the platform reports
// Failed, or every listed namespace if it reports none (a binding not yet
// reported per namespace).
func conflictedNamespaces(b *client.DatabaseBinding) []string {
	var failed []string
	for _, ns := range b.NamespaceStates() {
		if ns.Listed && ns.Reported && ns.Status.Phase == client.PhaseFailed {
			failed = append(failed, ns.Name)
		}
	}
	if len(failed) == 0 {
		return b.Namespaces
	}
	return failed
}

func newBindingListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list DATABASE",
		Short: "List the bindings of a database",
		Args:  cobra.ExactArgs(1),
		Example: `  kupe database binding list orders-db
  kupe db binding list orders-db -o wide`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListDatabaseBindings(cmd.Context(), args[0])
			if err != nil {
				return translateErr(err, args[0])
			}
			return printer.DatabaseBinding.List(f.IOStreams, format, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newBindingGetCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:     "get DATABASE BINDING",
		Short:   "Show a binding, including the Secret and each namespace's host",
		Args:    cobra.ExactArgs(2),
		Example: `  kupe database binding get orders-db orders-db-prod-readwrite`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, err := api.GetDatabaseBinding(cmd.Context(), args[0], args[1])
			if err != nil {
				return translateErr(err, args[0])
			}
			return printer.DatabaseBinding.One(f.IOStreams, format, b)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

type bindingUpdateOpts struct {
	add         []string
	remove      []string
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newBindingUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &bindingUpdateOpts{}

	cmd := &cobra.Command{
		Use:   "update DATABASE BINDING [--add-namespace NAMESPACE]... [--remove-namespace NAMESPACE]...",
		Short: "Add or remove namespaces of a binding",
		Long: `Change the namespaces a binding delivers into. A namespace added gets the
Service and credentials Secret; a namespace removed loses them and its network
path to the database, while the other namespaces keep working. A binding
lists 1 to 10 namespaces. Its cluster, role and Secret name cannot change.

Removing a namespace does not change the role's password; rotate it with
"kupe database rotate-credentials" if that namespace should also lose
knowledge of it.

The command reads the binding, edits its namespace list and writes it back.
If the binding changed in between, it reads it again and retries once. By
default it then waits until every listed namespace is Ready and every
removed one is withdrawn. Pass --wait=false to return as soon as the change
is accepted.`,
		Args: cobra.ExactArgs(2),
		Example: `  # Bind the readwrite role into the orders-worker namespace too
  kupe database binding update orders-db orders-db-prod-readwrite --add-namespace orders-worker

  # Stop binding it into orders-worker
  kupe db binding update orders-db orders-db-prod-readwrite --remove-namespace orders-worker

  # Move the staging binding from the API to the worker in one change, without waiting
  kupe db binding update orders-db orders-db-staging-readwrite --add-namespace orders-worker --remove-namespace orders-api --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name := args[0], args[1]
			if len(opts.add) == 0 && len(opts.remove) == 0 {
				return cli.MisuseError("pass --add-namespace or --remove-namespace")
			}
			for _, ns := range opts.add {
				if slices.Contains(opts.remove, ns) {
					return cli.MisuseError(fmt.Sprintf("namespace %s is both added and removed", ns))
				}
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
			updated, err := api.UpdateDatabaseBindingRMW(cmd.Context(), database, name, func(current *client.DatabaseBinding) *client.PatchDatabaseBindingRequest {
				namespaces, err := editNamespaces(current, database, opts.add, opts.remove)
				refused, unchanged = err, err == nil && slices.Equal(namespaces, current.Namespaces)
				if err != nil || unchanged {
					return nil
				}
				return &client.PatchDatabaseBindingRequest{Namespaces: namespaces}
			})
			if refused != nil {
				return refused
			}
			if err != nil {
				return translateBindingUpdateErr(err, database, name)
			}
			if unchanged {
				fmt.Fprintf(f.IOStreams.ErrOut, "binding %q is unchanged: it lists %s\n", name, strings.Join(updated.Namespaces, ", "))
			}
			if !opts.wait {
				return printer.DatabaseBinding.One(f.IOStreams, format, updated)
			}

			final, werr := cmdutil.WaitForPhase(cmd.Context(), f.IOStreams, bindingNoun, name, client.PhaseReady,
				bindingPhaseGetter(api, database, name), opts.waitTimeout)
			if werr != nil {
				return bindingWaitErr(werr, database, name, final, bindingUpdated)
			}
			return printer.DatabaseBinding.One(f.IOStreams, format, final)
		},
	}

	cmd.Flags().StringArrayVar(&opts.add, "add-namespace", nil, "Namespace to add; repeat for each namespace")
	cmd.Flags().StringArrayVar(&opts.remove, "remove-namespace", nil, "Namespace to remove; repeat for each namespace")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until every listed namespace is Ready and every removed one is withdrawn")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// editNamespaces is current's namespace list with add appended (those not
// listed yet, in order) and remove taken out. A namespace already listed,
// or one removed that is not listed, changes nothing. The result must list
// 1 to maxNamespacesPerBinding namespaces.
func editNamespaces(current *client.DatabaseBinding, database string, add, remove []string) ([]string, error) {
	namespaces := slices.DeleteFunc(slices.Clone(current.Namespaces), func(ns string) bool { return slices.Contains(remove, ns) })
	for _, ns := range add {
		if !slices.Contains(namespaces, ns) {
			namespaces = append(namespaces, ns)
		}
	}
	switch n := len(namespaces); {
	case n == 0:
		return nil, cli.MisuseError(fmt.Sprintf("binding %q must keep at least one namespace", current.Name)).
			WithHint(fmt.Sprintf("to remove the binding:  kupe database binding delete %s %s", database, current.Name))
	case n > maxNamespacesPerBinding:
		return nil, cli.MisuseError(fmt.Sprintf("a binding can list at most %d namespaces; this change would list %d", maxNamespacesPerBinding, n)).
			WithHint("bind the others with a second binding of the same role (binding add --name)")
	}
	return namespaces, nil
}

// translateBindingUpdateErr: a 409 terminating is the binding or its
// database being deleted (the API's message says which); the shared row's
// hint is about waiting for the database, so it is left out here.
func translateBindingUpdateErr(err error, database, name string) error {
	if client.ErrorCode(err) == client.CodeTerminating {
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("binding %q cannot change while it or its database is being deleted", name), err)
	}
	return translateErr(err, database)
}

func newBindingDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete DATABASE BINDING",
		Short: "Remove a binding",
		Long: `Remove a binding: the Service and credentials Secret leave every namespace
it lists, and their network path to the database closes. The role's password
does not change; rotate it with "kupe database rotate-credentials" if those
namespaces should also lose knowledge of it. To remove only some of its
namespaces, use "kupe database binding update --remove-namespace".

The request is asynchronous: the Services and Secrets are removed in the
background. Prompts on a TTY; pass --yes to skip (required in
non-interactive environments).`,
		Args:    cobra.ExactArgs(2),
		Example: `  kupe database binding delete orders-db orders-db-prod-readwrite --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name := args[0], args[1]
			io := f.IOStreams
			if err := cli.ConfirmDelete(io, yes, "database binding", name); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteDatabaseBinding(cmd.Context(), database, name); err != nil {
				return translateErr(err, database)
			}
			fmt.Fprintf(io.Out, "binding/%s delete requested\n", name)
			fmt.Fprintf(io.ErrOut, "The role's password is unchanged; to revoke it too: kupe database rotate-credentials %s --role ROLE\n", database)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
