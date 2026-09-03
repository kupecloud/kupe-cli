package tenant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

type deleteOpts struct {
	confirm     string
	cascade     bool
	wait        bool
	waitTimeout time.Duration
}

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	opts := &deleteOpts{waitTimeout: 30 * time.Minute}

	cmd := &cobra.Command{
		Use:   "delete --confirm NAME",
		Short: "Delete the current tenant (owner only)",
		Long: `Permanently delete the tenant the current context targets.

Only the tenant owner (the contact email shown by "kupe tenant get") can do
this, and only with an OIDC login — API keys are refused. --confirm must
equal the tenant name; there is no interactive prompt.

A tenant that still has clusters is refused unless --cascade is passed, in
which case every cluster is deleted with it. Deletion is asynchronous: the
API answers 202 and "kupe tenant get" reports Phase Terminating until the
tenant is gone (then exits 4). A tenant that was ever billed is held for its
final invoice, which can take up to 24 hours. Pass --wait to block until it
disappears.`,
		Example: `  kupe tenant delete --confirm acme
  kupe tenant delete --confirm acme --cascade
  kupe tenant delete --confirm acme --cascade --wait --wait-timeout 1h`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelete(cmd.Context(), f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.confirm, "confirm", "", "Tenant name, typed out to confirm (required; must equal the current context's tenant)")
	cmd.Flags().BoolVar(&opts.cascade, "cascade", false, "Also delete every cluster in the tenant")
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "Wait until the tenant is gone before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up waiting after this long (exit 8)")
	return cmd
}

func runDelete(ctx context.Context, f *cli.Factory, opts *deleteOpts) error {
	io := f.IOStreams

	r, err := f.Resolved()
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "resolving context", err)
	}
	if r.Tenant == "" {
		return cli.AuthError("no tenant set; pass --tenant, set KUPE_TENANT, or run kupe auth login")
	}
	name := r.Tenant

	// The API refuses API keys with 403 owner_required; say so before any
	// network call so the user learns what to do rather than what failed.
	if cli.IsAPIKeyAuth(r) {
		return cli.AuthError(fmt.Sprintf("deleting tenant %q requires the owner's OIDC login; API keys cannot delete a tenant", name)).
			WithHint(fmt.Sprintf("unset KUPE_API_TOKEN if set, then: kupe auth login --tenant %s   (device-code login as the tenant owner)", name))
	}

	confirm := strings.TrimSpace(opts.confirm)
	if confirm == "" {
		return cli.MisuseError("--confirm is required: type the tenant name to confirm deletion").
			WithHint(fmt.Sprintf("kupe tenant delete --confirm %s", name))
	}
	if confirm != name {
		return cli.MisuseError(fmt.Sprintf("--confirm %q does not match the current context's tenant %q", confirm, name))
	}

	api, err := f.Client()
	if err != nil {
		return err
	}

	var clusters []client.Cluster
	if opts.cascade {
		clusters, err = api.ListClusters(ctx)
		if err != nil {
			return cli.Wrap(cli.ExitCode(err), "listing clusters for --cascade", err)
		}
	}
	printDeleteTenantWarning(io, name, opts.cascade, clusters)

	if _, err := api.DeleteTenant(ctx, client.DeleteTenantRequest{Confirm: name, Cascade: opts.cascade}); err != nil {
		return mapDeleteErr(err, name)
	}
	fmt.Fprintf(io.Out, "tenant/%s terminating\n", name)

	if !opts.wait {
		return nil
	}
	if err := waitForGone(ctx, io, api, name, opts.waitTimeout); err != nil {
		return mapWaitErr(err, name)
	}
	return nil
}

// printDeleteTenantWarning writes the destructive-action summary to stderr
// (same stream the cluster delete warning uses) so it never pollutes a
// captured stdout. With --cascade it enumerates the clusters that go with
// the tenant — the one piece of state the user can't get back.
func printDeleteTenantWarning(io *cli.IOStreams, name string, cascade bool, clusters []client.Cluster) {
	fmt.Fprintf(io.ErrOut, "\nDeleting tenant %q will:\n", name)
	fmt.Fprintln(io.ErrOut, "  - Remove every member's access, all API keys, and all managed secrets.")
	fmt.Fprintln(io.ErrOut, "  - Issue a final invoice for any outstanding usage.")
	if cascade {
		if len(clusters) == 0 {
			fmt.Fprintln(io.ErrOut, "  - Delete its clusters (none exist).")
		} else {
			fmt.Fprintf(io.ErrOut, "  - Delete %d cluster(s), including every workload and volume inside them:\n", len(clusters))
			for _, c := range clusters {
				phase := ""
				if c.Status != nil && c.Status.Phase != "" {
					phase = " (" + c.Status.Phase + ")"
				}
				fmt.Fprintf(io.ErrOut, "      %s%s\n", c.Name, phase)
			}
		}
	}
	fmt.Fprintln(io.ErrOut)
}

// mapDeleteErr translates the API's DELETE /tenants/{tenant} refusals into
// typed CLI errors with a next step. Anything not matched keeps the client's
// default mapping (404 → 4, 401 → 3, 5xx → 1).
func mapDeleteErr(err error, name string) error {
	code := client.ErrorCode(err)
	switch {
	case client.IsForbidden(err):
		return cli.Wrap(cli.ExitAuth, fmt.Sprintf("only the tenant owner may delete tenant %q", name), err).
			WithHint("the owner is the contact email shown by \"kupe tenant get\"; log in with OIDC as that user (kupe auth login) and retry")
	case client.IsConflict(err) && code == client.TenantDeleteCodeClustersExist:
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("tenant %q still has clusters", name), err).
			WithHint(fmt.Sprintf("delete them first (kupe cluster list / kupe cluster delete NAME) or pass --cascade:\n  kupe tenant delete --confirm %s --cascade", name))
	case client.IsConflict(err) && code == client.TenantDeleteCodeAlreadyTerminating:
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("tenant %q is already being deleted", name), err).
			WithHint("\"kupe tenant get\" shows Phase Terminating until it is gone, then exits 4")
	case client.IsConflict(err):
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("cannot delete tenant %q", name), err)
	case client.IsRateLimited(err):
		return cli.Wrap(cli.ExitRateLimited, "tenant deletion is rate limited (one request per minute)", err).
			WithHint("wait a minute and retry")
	case client.IsValidation(err):
		return cli.Wrap(cli.ExitMisuse, "the API rejected the confirmation", err)
	}
	return err
}

// waitForGone polls GetTenant until it 404s. Transient failures (5xx, 429,
// transport) keep polling — the wait's own timeout bounds them; any typed
// 4xx other than 404 is terminal.
func waitForGone(ctx context.Context, streams *cli.IOStreams, api client.Interface, name string, timeout time.Duration) error {
	lastPhase := ""
	poll := func(ctx context.Context) (string, bool, error) {
		t, _, err := api.GetTenant(ctx)
		if err != nil {
			if client.IsNotFound(err) {
				return "Deleted", true, nil
			}
			if isTransientWaitErr(err) {
				return lastPhase, false, nil
			}
			return "", false, err
		}
		lastPhase = "Deleting"
		if t != nil && t.Status != nil && t.Status.Phase != "" && t.Status.Phase != client.PhaseTerminating {
			lastPhase = t.Status.Phase
		}
		return lastPhase, false, nil
	}
	return ux.WaitFor(ctx, streams, ux.WaitForOpts{
		Label:    "tenant " + name,
		DoneVerb: "deleted",
		Poll:     poll,
		Timeout:  timeout,
	})
}

func isTransientWaitErr(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, cli.ErrAuthRefreshTimeout) {
		return true
	}
	if client.IsUnavailable(err) || client.IsRateLimited(err) || client.IsServerError(err) {
		return true
	}
	return !client.IsAPIError(err)
}

// mapWaitErr mirrors cluster's wait mapping: timeout → exit 8, Ctrl-C →
// 130 with a reminder that deletion continues server-side.
func mapWaitErr(err error, name string) error {
	hint := "check status:  kupe tenant get   (exits 4 once the tenant is gone; a billed tenant can take up to 24h for its final invoice)"
	switch {
	case errors.Is(err, ux.ErrWaitTimeout):
		return cli.TimeoutError(fmt.Sprintf("timed out waiting for tenant %q to be deleted", name)).WithHint(hint)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting; tenant %q is still being deleted on Kupe Cloud", name)).WithHint(hint)
	}
	return err
}
