package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
	"github.com/spf13/cobra"
)

type bindingRotation struct {
	client.BindingCredentialRotation
	AppliedAt string `json:"appliedAt,omitempty" yaml:"appliedAt,omitempty"`
	Pending   bool   `json:"pending" yaml:"pending"`
}

func newBindingRotateCredentialsCmd(f *cli.Factory) *cobra.Command {
	opts := &rotateOpts{}
	cmd := &cobra.Command{
		Use:   "rotate-credentials DATABASE BINDING",
		Short: "Rotate a binding's dedicated credentials",
		Long: `Request new passwords for every namespace of a dedicated binding. Other
bindings keep their credentials. The old passwords stop working once the
rotation is applied. Applications must restart to pick up the new Secrets.
No password is shown.

By default, wait until status.rotation.appliedAt equals the requested token:
the passwords are applied and delivered to every listed namespace. Copying
the Secrets into the cluster follows asynchronously. If a newer rotation
replaces this one, the wait follows it. Pass --wait=false to return once the
request is accepted. Prompts on a TTY; pass --yes to skip (required in
non-interactive environments).`,
		Args:    cobra.ExactArgs(2),
		Example: "  kupe database binding rotate-credentials orders-db orders-db-prod-readwrite --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name := args[0], args[1]
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("This will rotate the dedicated passwords of binding %q on database %q. Applications must restart to pick up the new Secrets.", name, database)
			if err := cli.ConfirmAction(f.IOStreams, opts.yes, "rotate credentials", prompt); err != nil {
				return err
			}
			ack, err := api.RotateDatabaseBindingCredentials(cmd.Context(), database, name)
			if err != nil {
				return translateBindingUpdateErr(err, database, name)
			}
			result := bindingRotation{BindingCredentialRotation: *ack, Pending: true}
			if opts.wait {
				if err := waitForBindingRotation(cmd.Context(), f.IOStreams, api, database, name, &result, opts.waitTimeout); err != nil {
					return bindingRotationWaitErr(err, database, name)
				}
			}
			if format.Kind != printer.Table && format.Kind != printer.Wide {
				return printer.RenderOne(f.IOStreams.Out, format, &result, nil, func(r *bindingRotation) string { return r.Binding })
			}
			action := "credentials rotation requested"
			if !result.Pending {
				action = "credentials rotated"
			}
			fmt.Fprintf(f.IOStreams.Out, "binding/%s %s\n", name, action)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the new passwords are applied and delivered to every namespace")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 10*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func waitForBindingRotation(ctx context.Context, io *cli.IOStreams, api client.Interface, database, name string, result *bindingRotation, timeout time.Duration) error {
	if result.RequestedAt == "" {
		return cli.New(cli.ExitGeneral, "the rotation response has no request token")
	}
	lastPending := ""
	if err := ctx.Err(); err != nil {
		return err
	}
	get := bindingPhaseGetter(api, database, name)
	poll := cmdutil.PollPhase(get, func(b *client.DatabaseBinding, phase string) (bool, error) {
		if phase == client.PhaseTerminating {
			return false, &cmdutil.TerminalPhaseError{Noun: bindingNoun, Name: name, Phase: phase}
		}
		if b.Status == nil || b.Status.Rotation == nil {
			return false, nil
		}
		state := b.Status.Rotation
		if state.RequestedAt != "" && state.RequestedAt != result.RequestedAt {
			fmt.Fprintln(io.ErrOut, "A newer binding rotation replaced this one; waiting for it instead.")
			result.RequestedAt = state.RequestedAt
		}
		pending := []string{}
		for _, ns := range b.NamespaceStates() {
			if ns.Listed && (!ns.Reported || ns.Status.DeliveredRotation != result.RequestedAt) {
				pending = append(pending, ns.Name)
			}
		}
		names := strings.Join(pending, ", ")
		if names != "" && names != lastPending {
			fmt.Fprintf(io.ErrOut, "Waiting for credentials in namespaces: %s\n", names)
		}
		lastPending = names
		done := state.AppliedAt == result.RequestedAt && !state.Pending && b.Status.ObservedRotation == result.RequestedAt && len(pending) == 0
		if done {
			result.AppliedAt = state.AppliedAt
			result.Pending = false
		}
		return done, nil
	})
	return ux.WaitFor(ctx, io, ux.WaitForOpts{Label: "binding " + name, DoneVerb: "credentials rotated", PhaseOverride: "Rotating", Poll: poll, Timeout: timeout})
}

func bindingRotationWaitErr(err error, database, name string) error {
	check := fmt.Sprintf("check progress:  kupe database binding get %s %s -o yaml", database, name)
	switch {
	case errors.Is(err, ux.ErrWaitTimeout):
		return cli.TimeoutError("the binding's credentials rotation is not in effect yet").WithHint("the rotation was requested and continues\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, "stopped waiting; the binding's credentials rotation continues").WithHint(check)
	}
	mapped := cmdutil.MapWaitErr(err, bindingNoun, name, "wait")
	var ce *cli.Error
	if errors.As(mapped, &ce) {
		return ce.WithHint(check)
	}
	return cli.WrapAPI(cli.ExitCode(mapped), "waiting for the binding's credentials rotation failed", mapped).WithHint(check)
}
