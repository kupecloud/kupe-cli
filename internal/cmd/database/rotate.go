package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

type rotateOpts struct {
	role        string
	yes         bool
	wait        bool
	waitTimeout time.Duration
	output      string
}

// rotation is what rotate-credentials reports with -o: the request kupe-api
// acknowledged and, once a wait has seen it applied, the token applied.
type rotation struct {
	Database    string `json:"database" yaml:"database"`
	Role        string `json:"role" yaml:"role"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
	AppliedAt   string `json:"appliedAt,omitempty" yaml:"appliedAt,omitempty"`
	Pending     bool   `json:"pending" yaml:"pending"`
	// Delivered is true once the wait saw every binding of the role carry
	// the new password.
	Delivered bool `json:"delivered,omitempty" yaml:"delivered,omitempty"`
}

func newRotateCredentialsCmd(f *cli.Factory) *cobra.Command {
	opts := &rotateOpts{wait: true, waitTimeout: 10 * time.Minute}

	cmd := &cobra.Command{
		Use:   "rotate-credentials NAME --role owner|readwrite|readonly",
		Short: "Rotate the password of one database role",
		Long: `Request a new password for one database role: owner (schema changes),
readwrite (read and write data) or readonly (read data), and by default wait
until it is in effect. Each role has its own password; rotating one leaves
the others unchanged.

The wait ends once the new password is in effect in PostgreSQL (the old one
then stops working) and every binding of that role carries it in its
credentials Secret. The Secret reaches the cluster within seconds after that;
applications using the old password must restart to pick it up. No password
is shown.

Pass --wait=false to return as soon as the rotation is requested. In
"kupe database get NAME -o yaml", the rotation is in effect once
status.rotations.ROLE.appliedAt equals the requestedAt this command printed
(-o yaml shows it) and status.rotations.ROLE.delivered is true. If another
rotation of the same role is requested meanwhile, it replaces this one and the
wait follows it.

Rotate after removing a binding if that namespace should also lose knowledge
of the password. Prompts on a TTY; pass --yes to skip (required in
non-interactive environments).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database rotate-credentials orders-db --role readwrite
  kupe db rotate-credentials orders-db --role owner --yes
  kupe db rotate-credentials orders-db --role readonly --yes --wait=false -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams
			if err := validateRole(opts.role); err != nil {
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

			fmt.Fprintf(io.ErrOut, `
Rotating the %s password of database %q:
  - The current password stops working once the rotation completes.
  - Bound credentials Secrets are updated; applications using the old password must restart.

`, opts.role, name)
			prompt := fmt.Sprintf("This will rotate the %s password of database %q.", opts.role, name)
			if err := cli.ConfirmAction(io, opts.yes, "rotate credentials", prompt); err != nil {
				return err
			}
			ack, err := api.RotateDatabaseCredentials(cmd.Context(), name, opts.role)
			if err != nil {
				return translateRotateErr(err, name)
			}
			result := rotation{Database: name, Role: opts.role, RequestedAt: ack.RequestedAt, Pending: true}

			waited := false
			if opts.wait {
				applied, observable, werr := waitForRotation(cmd.Context(), io, api, name, opts.role, &result, opts.waitTimeout)
				switch {
				case werr != nil:
					return rotateWaitErr(werr, name, opts.role)
				case !observable:
					fmt.Fprintf(io.ErrOut, "kupe-api does not report %s rotation progress yet, so the command cannot wait for it; the rotation was requested.\n", opts.role)
				default:
					result.AppliedAt, result.Pending, result.Delivered, waited = applied, false, true, true
				}
			}

			if format.Kind != printer.Table && format.Kind != printer.Wide {
				return printer.RenderOne(io.Out, format, &result, nil, func(r *rotation) string { return r.Database })
			}
			if waited {
				fmt.Fprintf(io.Out, "database/%s %s credentials rotated\n", name, opts.role)
				return nil
			}
			fmt.Fprintf(io.Out, "database/%s %s credentials rotation requested\n", name, opts.role)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.role, "role", "", "Role whose password is rotated: owner, readwrite or readonly (required)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the new password is in effect before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 10*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: table | json | yaml | go-template=...")
	return cmd
}

// waitForRotation polls the database until the rotation requested with
// result.RequestedAt is in effect (client.RotationStatus.InEffect): applied,
// and delivered to every binding of the role. The tokens are opaque, so
// only equality counts. A rotation of the same role requested meanwhile
// replaces this one (the requested token moves on): the wait then follows
// that one, says so once, and result.RequestedAt is updated. observable is
// false when the platform does not report rotations, or does not report
// this role's (an older kupe-api), or acknowledged no token, so there is
// nothing to wait for: a role the server does not report is never waited
// on as if its zero value were a rotation in progress. A database that
// starts being deleted ends the wait.
func waitForRotation(ctx context.Context, io *cli.IOStreams, api client.Interface, name, role string, result *rotation, timeout time.Duration) (applied string, observable bool, err error) {
	if result.RequestedAt == "" {
		return "", false, nil
	}
	// One read first: a platform that reports no rotations is not waited on
	// at all, so the renderer never prints a success line for it. A
	// transient failure (a 503 while kupe-api restarts) is left to the
	// polling below, which rides it out.
	get := service.Getter(api, name)
	db, _, err := get(ctx)
	switch {
	case err != nil && !cmdutil.IsTransientWaitErr(err):
		return "", true, err
	case err == nil && !reportsRotation(db, role):
		return "", false, nil
	}
	poll := cmdutil.PollPhase(get, func(db *client.Database, phase string) (bool, error) {
		if phase == client.PhaseTerminating {
			return false, &cmdutil.TerminalPhaseError{Noun: noun, Name: name, Phase: phase}
		}
		if !reportsRotation(db, role) {
			return false, nil
		}
		state, _ := db.Status.Rotations.Role(role)
		if state.RequestedAt != "" && state.RequestedAt != result.RequestedAt {
			fmt.Fprintf(io.ErrOut, "A newer %s rotation (%s) replaced this one; waiting for it instead.\n", role, state.RequestedAt)
			result.RequestedAt = state.RequestedAt
		}
		applied = state.AppliedAt
		return state.InEffect(result.RequestedAt), nil
	})
	err = ux.WaitFor(ctx, io, ux.WaitForOpts{
		Label:         fmt.Sprintf("database %s %s", name, role),
		DoneVerb:      "credentials rotated",
		PhaseOverride: "Rotating",
		Poll:          poll,
		Timeout:       timeout,
	})
	return applied, true, err
}

// reportsRotation reports whether db carries the rotation state of role.
func reportsRotation(db *client.Database, role string) bool {
	if db == nil || db.Status == nil {
		return false
	}
	_, ok := db.Status.Rotations.Role(role)
	return ok
}

// rotateWaitErr explains why waiting for a rotation ended before it was in
// effect. The rotation was requested in every case and the platform keeps
// applying it. There is no command that resumes a rotation wait, so every
// hint points at the rotation status, never at "kupe database wait".
func rotateWaitErr(err error, name, role string) error {
	check := fmt.Sprintf("check status:  kupe database get %s -o yaml   (status.rotations.%s: in effect once appliedAt equals requestedAt and delivered is true)", name, role)
	switch {
	case errors.Is(err, ux.ErrWaitTimeout):
		return cli.TimeoutError(fmt.Sprintf("the %s password rotation of database %q is not in effect yet", role, name)).
			WithHint("the rotation was requested and the platform keeps applying it\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting; the %s password rotation of database %q was requested and continues", role, name)).
			WithHint(check)
	}
	mapped := cmdutil.MapWaitErr(err, noun, name, "wait")
	var ce *cli.Error
	if errors.As(mapped, &ce) {
		return ce.WithHint(check)
	}
	// A bare API refusal (the database deleted mid-wait) keeps its exit code.
	return cli.WrapAPI(cli.ExitCode(mapped), fmt.Sprintf("waiting for the %s password rotation of database %q failed", role, name), mapped).WithHint(check)
}
