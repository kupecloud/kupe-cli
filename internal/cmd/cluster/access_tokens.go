package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/kubeconfig"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// Access token expiry bounds, as the platform publishes them
// (services.cluster.accessTokenExpiryDays); kupe-api is authoritative.
const (
	minAccessTokenDays = 1
	maxAccessTokenDays = 365
)

// accessTokenPollInterval and accessTokenWaitTimeout bound the wait for the
// cluster to issue a token (seconds, normally). Variables so tests run
// fast.
var (
	accessTokenPollInterval = 2 * time.Second
	accessTokenWaitTimeout  = 2 * time.Minute
)

// errAccessTokenNotIssued is a token the cluster did not issue in time;
// errAccessTokenNotUsable one it could not issue at all (phase Failed).
var (
	errAccessTokenNotIssued = errors.New("not issued in time")
	errAccessTokenNotUsable = errors.New("could not be issued")
)

func newAccessTokenCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access-token",
		Aliases: []string{"access-tokens"},
		Short:   "Manage a cluster's access tokens for automation",
		Long: `Create, list and revoke cluster access tokens: kubeconfig credentials for
Terraform, CI and other automation that runs kubectl or a Kubernetes client
against one cluster without a person signed in. A token acts as the admin
role inside the cluster until it expires, 1 to 365 days after creation, or
is revoked.

Tokens belong to the tenant: removing or demoting the member who created one
does not revoke it. Every member can list them; only tenant admins can
create and revoke them. A cluster holds at most 20.`,
	}
	cmd.AddCommand(newAccessTokenCreateCmd(f))
	cmd.AddCommand(newAccessTokenListCmd(f))
	cmd.AddCommand(newAccessTokenGetCmd(f))
	cmd.AddCommand(newAccessTokenRevokeCmd(f))
	return cmd
}

// accessTokenCreateOutput is `access-tokens create -o json`: the token's
// details, the token and its kubeconfig.
type accessTokenCreateOutput struct {
	ID         string `json:"id"`
	Cluster    string `json:"cluster"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  string `json:"createdAt"`
	ExpiresAt  string `json:"expiresAt"`
	Token      string `json:"token"`
	Kubeconfig string `json:"kubeconfig"`
}

type accessTokenCreateOpts struct {
	name           string
	role           string
	days           int
	waitTimeout    time.Duration
	output         string
	merge          bool
	contextName    string
	userName       string
	clusterName    string
	force          bool
	forceOverwrite bool
}

func newAccessTokenCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &accessTokenCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create CLUSTER --name NAME",
		Short: "Create an access token and print its kubeconfig",
		Long: `Create an access token for the cluster, wait until the cluster has issued it
(seconds; at most --wait-timeout), and print a kubeconfig that uses it on
stdout, once. Store it somewhere safe now: it cannot be shown again. On a
TTY the token's ID, expiry and the command that revokes it follow on stderr;
otherwise stdout holds only the kubeconfig, so
"kupe cluster access-tokens create dev --name ci > ci.kubeconfig" works.

--merge writes it into $KUBECONFIG (or ~/.kube/config) as the context
kupe-<tenant>-<cluster>-<id> instead of printing it. -o json prints the
token's details, the token and the kubeconfig.

--expires-in-days is 1 to 365 (default 90). A token that is not issued in
time, or cannot be handed over, is revoked.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Kubeconfig for a CI pipeline
  kupe cluster access-tokens create prod --name github-actions > ci.kubeconfig

  # Valid 30 days, merged into ~/.kube/config
  kupe cluster access-tokens create prod --name laptop-break-glass --expires-in-days 30 --merge`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessTokenCreate(cmd, f, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "A name to recognise the token by, 1 to 128 characters (required)")
	cmd.Flags().StringVar(&opts.role, "role", client.ClusterAccessTokenRoleAdmin, "The role the token acts as inside the cluster: admin")
	cmd.Flags().IntVar(&opts.days, "expires-in-days", 90, "Days until the token stops working, 1 to 365")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", accessTokenWaitTimeout, "Give up and revoke the token if the cluster has not issued it after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpToggle)
	cmd.Flags().BoolVar(&opts.merge, "merge", false, "Merge into $KUBECONFIG or ~/.kube/config instead of printing")
	cmd.Flags().StringVar(&opts.contextName, "context-name", "", "Context name for --merge (default: kupe-<tenant>-<cluster>-<id>)")
	cmd.Flags().StringVar(&opts.userName, "user-name", "", "User entry name for --merge (default: same as context-name)")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster entry name for --merge (default: same as context-name)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Overwrite colliding entries on --merge")
	cmd.Flags().BoolVar(&opts.forceOverwrite, "force-overwrite", false, "If the existing kubeconfig is corrupt, discard it and start fresh (this loses its contents; use with care)")
	return cmd
}

// request checks the flags and builds the create body; --expires-in-days
// is sent only when given, so the platform's default applies otherwise.
func (o *accessTokenCreateOpts) request(cmd *cobra.Command) (client.CreateClusterAccessTokenRequest, error) {
	req := client.CreateClusterAccessTokenRequest{DisplayName: o.name, Access: client.ClusterAccessTokenAccess{Role: o.role}}
	if err := cmdutil.CheckTokenName(o.name, "terraform"); err != nil {
		return req, err
	}
	if o.role != client.ClusterAccessTokenRoleAdmin {
		return req, cli.MisuseError(fmt.Sprintf("invalid --role %q", o.role)).WithHint("v1 tokens are admin only")
	}
	if cmd.Flags().Changed("expires-in-days") {
		if o.days < minAccessTokenDays || o.days > maxAccessTokenDays {
			return req, cli.MisuseError(fmt.Sprintf("--expires-in-days must be from %d to %d", minAccessTokenDays, maxAccessTokenDays))
		}
		days := o.days
		req.ExpiresInDays = &days
	}
	switch o.output {
	case "", "text":
	case "json":
		if o.merge {
			return req, cli.MisuseError("-o json and --merge are mutually exclusive")
		}
	default:
		return req, cli.MisuseError(fmt.Sprintf("unsupported output format %q (expected one of: text, json)", o.output))
	}
	if !o.merge && (o.contextName != "" || o.userName != "" || o.clusterName != "" || o.force || o.forceOverwrite) {
		return req, cli.MisuseError("--context-name, --user-name, --cluster-name, --force and --force-overwrite need --merge")
	}
	return req, nil
}

// mergeNames is where --merge writes the token: kupe-<tenant>-<cluster>-<id>
// unless overridden, with the overrides of "kupe cluster kubeconfig".
func (o *accessTokenCreateOpts) mergeNames(tenant, cluster, id string) kubeconfig.Names {
	return resolveNames(tenant, cluster+"-"+id, &kubeconfigOpts{contextName: o.contextName, userName: o.userName, clusterName: o.clusterName})
}

func runAccessTokenCreate(cmd *cobra.Command, f *cli.Factory, cluster string, opts *accessTokenCreateOpts) error {
	req, err := opts.request(cmd)
	if err != nil {
		return err
	}
	api, err := f.Client()
	if err != nil {
		return err
	}
	// A merge that would be refused is refused before the token exists:
	// the token can be fetched only once.
	if opts.merge {
		if err := checkMergeable(opts, opts.mergeNames(api.Tenant(), cluster, "")); err != nil {
			return err
		}
	}
	ctx := cmd.Context()
	created, err := api.CreateClusterAccessToken(ctx, cluster, req)
	if err != nil {
		return translateAccessTokenErr(err, cluster)
	}
	collected, last, err := waitAndCollect(ctx, api, cluster, created.ID, opts.waitTimeout)
	if err != nil {
		return notCollectedErr(err, cluster, created.ID, last, revokeAccessTokenQuietly(ctx, api, cluster, created.ID))
	}
	if opts.merge {
		if err := mergeAccessToken(f, api.Tenant(), cluster, opts, collected); err != nil {
			return storeFailedErr(err, cluster, created.ID, revokeAccessTokenQuietly(ctx, api, cluster, created.ID))
		}
		return nil
	}
	expiresAt := collected.ExpiresAt
	if expiresAt == "" {
		expiresAt = created.ExpiresAt
	}
	if opts.output == "json" {
		enc := json.NewEncoder(f.IOStreams.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(accessTokenCreateOutput{ // #nosec G117 -- -o json prints the token on stdout: the user asked for it
			ID: created.ID, Cluster: cluster, Name: created.DisplayName, Role: created.Access.Role,
			CreatedBy: created.CreatedBy, CreatedAt: created.CreatedAt, ExpiresAt: expiresAt,
			Token: collected.Token, Kubeconfig: collected.Kubeconfig,
		})
	}
	return renderAccessTokenCreated(f.IOStreams, cluster, created.ID, expiresAt, collected.Kubeconfig)
}

// checkMergeable refuses a --merge target that already has an entry of
// the token's names (unless --force) or cannot be parsed (unless
// --force-overwrite). With the default names the id is not known yet, so
// only overridden names can collide.
func checkMergeable(opts *accessTokenCreateOpts, names kubeconfig.Names) error {
	target, err := kubeconfig.TargetPath("")
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "resolving kubeconfig path", err)
	}
	if opts.contextName == "" {
		names.Context = ""
	}
	if opts.userName == "" && opts.contextName == "" {
		names.User = ""
	}
	if opts.clusterName == "" && opts.contextName == "" {
		names.Cluster = ""
	}
	return mergeErr(kubeconfig.CheckMergeable(target, names, kubeconfig.MergeOptions{Force: opts.force, ForceOverwrite: opts.forceOverwrite}), target)
}

// mergeAccessToken writes the collected token into the user's kubeconfig.
// The token reaches the file and nothing else.
func mergeAccessToken(f *cli.Factory, tenant, cluster string, opts *accessTokenCreateOpts, collected *client.CollectedClusterAccessToken) error {
	names := opts.mergeNames(tenant, cluster, collected.ID)
	built, err := kubeconfig.BuildTokenConfig(names, collected.Endpoint, collected.CertificateAuthority, collected.Token)
	if err != nil {
		return cli.Wrap(cli.ExitGeneral, "building kubeconfig", err)
	}
	return mergeKubeconfig(f, names, built, opts.force, opts.forceOverwrite)
}

// renderAccessTokenCreated writes the kubeconfig alone to stdout and, on a
// TTY, the token's ID, expiry and how to revoke it to stderr.
func renderAccessTokenCreated(io *cli.IOStreams, cluster, id, expiresAt, kubeconfigYAML string) error {
	if _, err := fmt.Fprint(io.Out, kubeconfigYAML); err != nil {
		return err
	}
	if !io.IsStderrTTY() {
		return nil
	}
	w := io.ErrOut
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Store this kubeconfig now: it is shown only once.\n\n")
	fmt.Fprintf(w, "  ID:       %s\n", id)
	fmt.Fprintf(w, "  Expires:  %s\n", expiresAt)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Revoke with: %s\n", revokeCommand(cluster, id, false))
	return nil
}

// waitAndCollect polls the token until the cluster has issued it (phase
// Ready), then collects it. A collect answered ACCESS_TOKEN_NOT_READY (a
// race with the platform) or a transient failure goes back to waiting,
// within the same deadline. It returns the token as last read with any
// error, and prints nothing.
func waitAndCollect(ctx context.Context, api client.Interface, cluster, id string, timeout time.Duration) (*client.CollectedClusterAccessToken, *client.ClusterAccessToken, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var last *client.ClusterAccessToken
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, last, errAccessTokenNotIssued
			}
			return nil, last, ctx.Err()
		case <-timer.C:
		}
		t, err := api.GetClusterAccessToken(ctx, cluster, id)
		switch {
		case err != nil && ctx.Err() != nil:
			// Cut short by the wait's own end; the next turn reports it.
		case err != nil && !isTransient(err):
			return nil, last, err
		case err != nil:
		case t.Phase() == client.PhaseFailed:
			return nil, t, errAccessTokenNotUsable
		case t.Phase() == client.PhaseReady:
			last = t
			collected, cerr := api.CollectClusterAccessToken(ctx, cluster, id)
			switch {
			case cerr == nil:
				return collected, t, nil
			case ctx.Err() != nil, client.ErrorCode(cerr) == client.ClusterAccessTokenCodeNotReady, isTransient(cerr):
				// Not yet, or cut short: wait again.
			case client.ErrorCode(cerr) == client.ClusterAccessTokenCodeFailed:
				return nil, t, errAccessTokenNotUsable
			default:
				return nil, t, cerr
			}
		default:
			last = t
		}
		timer.Reset(accessTokenPollInterval)
	}
}

// isTransient reports whether err is worth another poll: a 5xx, a 429 or a
// failure that never reached kupe-api.
func isTransient(err error) bool {
	return client.IsServerError(err) || client.IsRateLimited(err) || !client.IsAPIError(err)
}

// revokeAccessTokenQuietly revokes a token that will never be used (it was
// not handed over), even after the command's context ended. A token
// already gone counts as revoked. It returns the failure for the error
// message.
func revokeAccessTokenQuietly(ctx context.Context, api client.Interface, cluster, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := api.DeleteClusterAccessToken(ctx, cluster, id); err != nil && !client.IsNotFound(err) {
		return err
	}
	return nil
}

// revokeCommand is the command that revokes the token.
func revokeCommand(cluster, id string, yes bool) string {
	c := "kupe cluster access-tokens revoke " + cluster + " " + id
	if yes {
		c += " --yes"
	}
	return c
}

// revokedClause says the token was revoked, or how to revoke it when that
// failed.
func revokedClause(cluster, id string, revokeErr error) string {
	if revokeErr == nil {
		return "it was revoked"
	}
	return fmt.Sprintf("revoking it failed (%v); revoke it with:  %s", revokeErr, revokeCommand(cluster, id, true))
}

// createAgainHint is the next step once a created token was revoked.
func createAgainHint(cluster string) string {
	return "try again:  kupe cluster access-tokens create " + cluster + " --name NAME"
}

// notCollectedErr explains why a created token was not handed over; it was
// revoked unless revokeErr says otherwise.
func notCollectedErr(err error, cluster, id string, last *client.ClusterAccessToken, revokeErr error) error {
	revoked := revokedClause(cluster, id, revokeErr)
	switch {
	case errors.Is(err, errAccessTokenNotIssued):
		return cli.New(cli.ExitTimeout, "the cluster did not issue the access token in time; "+revoked).WithHint(createAgainHint(cluster))
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, "stopped waiting for the access token; "+revoked)
	case errors.Is(err, errAccessTokenNotUsable):
		msg := "the cluster could not issue the access token"
		if last != nil && last.Status != nil && last.Status.Message != "" {
			msg += ": " + last.Status.Message
		}
		return cli.New(cli.ExitGeneral, msg+"; "+revoked).WithHint(createAgainHint(cluster))
	case client.ErrorCode(err) == client.ClusterAccessTokenCodeAlreadyCollected:
		if revokeErr == nil {
			return cli.New(cli.ExitConflict, "the access token was already collected elsewhere; it was revoked. Create another.")
		}
		return cli.New(cli.ExitConflict, "the access token was already collected elsewhere; "+revoked)
	}
	translated := translateAccessTokenErr(err, cluster)
	out := &cli.Error{Code: cli.ExitCode(translated), Msg: "the access token was not handed over; " + revoked, Err: translated}
	var ce *cli.Error
	if errors.As(translated, &ce) && ce.Hint != "" {
		out.Hint = ce.Hint
	}
	return out
}

// storeFailedErr is a collected token that --merge could not write: lost,
// so revoked.
func storeFailedErr(err error, cluster, id string, revokeErr error) error {
	out := &cli.Error{Code: cli.ExitCode(err), Msg: "the kubeconfig could not be written, so the access token was lost; " + revokedClause(cluster, id, revokeErr), Err: err}
	var ce *cli.Error
	if errors.As(err, &ce) && ce.Hint != "" {
		out.Hint = ce.Hint
	}
	return out
}

func newAccessTokenListCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list CLUSTER",
		Short: "List a cluster's access tokens",
		Long: `List the cluster's access tokens, newest first: ID, name, role, who created
it, expiry and status (Pending, Ready, Collected, Failed or Revoking). Tokens
are never shown. Every member can list.`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe cluster access-tokens list prod`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListClusterAccessTokens(cmd.Context(), args[0])
			if err != nil {
				return translateAccessTokenErr(err, args[0])
			}
			return printer.ClusterAccessToken.List(f.IOStreams, format, list)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newAccessTokenGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "get CLUSTER ID",
		Short:   "Show one of a cluster's access tokens",
		Long:    `Show one of the cluster's access tokens. The token itself is never shown.`,
		Args:    cobra.ExactArgs(2),
		Example: `  kupe cluster access-tokens get prod cat-0a1b2c3d4e5f6a7b`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			t, err := api.GetClusterAccessToken(cmd.Context(), args[0], args[1])
			if err != nil {
				return translateAccessTokenErr(err, args[0])
			}
			return printer.ClusterAccessToken.One(f.IOStreams, format, t)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newAccessTokenRevokeCmd(f *cli.Factory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "revoke CLUSTER ID",
		Short: "Revoke one of a cluster's access tokens",
		Long: `Revoke an access token: it stops working within seconds. Only tenant admins
can revoke.

Prompts on a TTY; pass --yes to skip (required in non-interactive
environments).`,
		Args:    cobra.ExactArgs(2),
		Example: `  kupe cluster access-tokens revoke prod cat-0a1b2c3d4e5f6a7b --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, id := args[0], args[1]
			io := f.IOStreams
			if err := cli.ConfirmAction(io, yes, "revoke", fmt.Sprintf("This will revoke access token %q of cluster %q.", id, cluster)); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteClusterAccessToken(cmd.Context(), cluster, id); err != nil {
				return translateAccessTokenErr(err, cluster)
			}
			fmt.Fprintf(io.Out, "access-token/%s revoked\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}

// accessTokenRefusal is the CLI's answer to one access token refusal: the
// exit code, the CLI's own sentence for when the API sent no message, and
// a hint.
type accessTokenRefusal struct {
	exit     int
	fallback string
	hint     func(cluster string) string
}

// accessTokenRefusals maps kupe-api's access token codes (lower-case, as
// client.ErrorCode returns them). ACCESS_TOKEN_NOT_READY never reaches
// here: create's wait absorbs it.
var accessTokenRefusals = map[string]accessTokenRefusal{
	client.ClusterAccessTokenCodeLimitReached: {cli.ExitConflict, "the cluster has as many access tokens as it can hold", func(cluster string) string {
		return "revoke one first; to list them:  kupe cluster access-tokens list " + orCluster(cluster)
	}},
	client.ClusterAccessTokenCodeExpiryInvalid: {cli.ExitMisuse, "the expiry is out of range", nil},
	client.ClusterAccessTokenCodeAlreadyCollected: {cli.ExitConflict, "the access token was already collected", func(string) string {
		return "revoke it and create another"
	}},
	client.ClusterAccessTokenCodeFailed: {cli.ExitGeneral, "the access token could not be issued", func(string) string {
		return "revoke it and create another"
	}},
	client.ClusterAccessTokenCodeCollectCreatorOnly: {cli.ExitAuth, "only the member or API key that created an access token can collect it", nil},
	// A change refused while the cluster is being deleted, or the token
	// revoked: the API's sentence says which.
	client.CodeTerminating: {cli.ExitConflict, "it is being deleted", nil},
}

func orCluster(cluster string) string {
	if cluster == "" {
		return "CLUSTER"
	}
	return cluster
}

// translateAccessTokenErr maps an access token refusal onto a CLI error:
// the exit code by refusal class, the API's message as the text and a next
// step. A 403 without a code is a member who is not an admin. The rest
// are the refusals every cluster command shares.
func translateAccessTokenErr(err error, cluster string) error {
	if err == nil {
		return nil
	}
	if r, ok := accessTokenRefusals[client.ErrorCode(err)]; ok {
		out := cli.WrapAPI(r.exit, r.fallback, err)
		if r.hint != nil {
			out = out.WithHint(r.hint(cluster))
		}
		return out
	}
	if client.IsForbidden(err) && client.ErrorCode(err) == "" {
		return cli.WrapAPI(cli.ExitAuth, "admin access required", err).
			WithHint("only tenant admins can create and revoke cluster access tokens")
	}
	return cmdutil.TranslateCommon("cluster", cluster, err)
}
