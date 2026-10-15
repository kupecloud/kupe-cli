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
}

func newRotateCredentialsCmd(f *cli.Factory) *cobra.Command {
	opts := &rotateOpts{wait: true, waitTimeout: 10 * time.Minute}

	cmd := &cobra.Command{
		Use:   "rotate-credentials NAME --role owner|app",
		Short: "Rotate the password of one database role",
		Long: `Request a new password for one database role: owner (schema changes) or
app (read and write data), and by default wait until it is in effect.

Once the rotation is in effect the current password stops working and every
bound credentials Secret for that role carries the new one; applications
using the old password must restart to pick it up. No password is shown.

Pass --wait=false to return as soon as the rotation is requested. The
rotation is in effect once status.rotations.ROLE.appliedAt in
"kupe database get NAME -o yaml" equals the requestedAt this command
printed (-o yaml shows it).

Rotate after removing a binding if that namespace should also lose knowledge
of the password. Prompts on a TTY; pass --yes to skip (required in
non-interactive environments).`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe database rotate-credentials orders-db --role app
  kupe db rotate-credentials orders-db --role owner --yes
  kupe db rotate-credentials orders-db --role app --yes --wait=false -o json`,
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
				applied, observable, werr := waitForRotation(cmd.Context(), io, api, name, opts.role, ack.RequestedAt, opts.waitTimeout)
				switch {
				case werr != nil:
					return rotateWaitErr(werr, name, opts.role)
				case !observable:
					fmt.Fprintln(io.ErrOut, "kupe-api does not report rotation progress yet, so the command cannot wait for it; the rotation was requested.")
				default:
					result.AppliedAt, result.Pending, waited = applied, false, true
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

	cmd.Flags().StringVar(&opts.role, "role", "", "Role whose password is rotated: owner or app (required)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the new password is in effect before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 10*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: table | json | yaml | go-template=...")
	return cmd
}

// waitForRotation polls the database until the rotation requested with
// token is in effect: status.rotations.{role}.appliedAt equals token. The
// tokens are opaque, so only equality counts; a later rotation requested by
// someone else replaces the token, and this wait then times out. observable
// is false when the platform does not report rotations (an older kupe-api)
// or acknowledged no token, so there is nothing to wait for. A database
// that starts being deleted ends the wait.
func waitForRotation(ctx context.Context, io *cli.IOStreams, api client.Interface, name, role, token string, timeout time.Duration) (applied string, observable bool, err error) {
	if token == "" {
		return "", false, nil
	}
	observable = true
	poll := cmdutil.PollPhase(service.Getter(api, name), func(db *client.Database, phase string) (bool, error) {
		if phase == client.PhaseTerminating {
			return false, &cmdutil.TerminalPhaseError{Noun: noun, Name: name, Phase: phase}
		}
		if db.Status == nil || db.Status.Rotations == nil {
			observable = false
			return true, nil
		}
		applied = db.Status.Rotations.Role(role).AppliedAt
		return applied == token, nil
	})
	err = ux.WaitFor(ctx, io, ux.WaitForOpts{
		Label:         fmt.Sprintf("database %s %s", name, role),
		DoneVerb:      "credentials rotated",
		PhaseOverride: "Rotating",
		Poll:          poll,
		Timeout:       timeout,
	})
	return applied, observable, err
}

// rotateWaitErr explains why waiting for a rotation ended before it was in
// effect. The rotation was requested in every case and the platform keeps
// applying it.
func rotateWaitErr(err error, name, role string) error {
	check := fmt.Sprintf("check status:  kupe database get %s -o yaml   (status.rotations.%s: appliedAt equals requestedAt once in effect)", name, role)
	switch {
	case errors.Is(err, ux.ErrWaitTimeout):
		return cli.TimeoutError(fmt.Sprintf("the %s password of database %q is not rotated yet; the old one still works", role, name)).
			WithHint("the rotation was requested and the platform keeps applying it; another rotation requested since replaces this one\n" + check)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting; the %s password rotation of database %q was requested and continues", role, name)).
			WithHint(check)
	}
	return cmdutil.MapWaitErr(err, noun, name, "wait")
}
