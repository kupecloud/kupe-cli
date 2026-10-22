package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// Token expiry bounds, as the platform publishes them
// (services.registry.tokenExpiryDays); kupe-api is authoritative.
const (
	minTokenDays = 1
	maxTokenDays = 365
)

// issuePollInterval and issueTimeout bound the wait for the platform to
// issue a token or session (seconds, normally). Variables so tests run fast.
var (
	issuePollInterval = 2 * time.Second
	issueTimeout      = 60 * time.Second
)

// errNotIssued is a token the platform did not issue in time; errNotUsable
// one it could not issue at all (phase Failed).
var (
	errNotIssued = errors.New("not issued in time")
	errNotUsable = errors.New("could not be issued")
)

// waitIssued polls the token until the platform has issued it (phase Ready,
// username set) and returns it as last read. A Failed token is returned with
// an error. It prints nothing, so a command's stdout and stderr stay as
// scripts expect.
func waitIssued(ctx context.Context, api client.Interface, id string, timeout time.Duration) (*client.RegistryToken, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var last *client.RegistryToken
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return last, errNotIssued
			}
			return last, ctx.Err()
		case <-timer.C:
		}
		t, err := api.GetRegistryToken(ctx, client.RegistryName, id)
		switch {
		case err != nil && ctx.Err() != nil:
			// Cut short by the wait's own end; the next turn reports it.
		case err != nil && !isTransient(err):
			return last, err
		case err == nil && t.Phase() == client.PhaseFailed:
			return t, errNotUsable
		case err == nil && t.Phase() == client.PhaseReady && t.Username != "":
			return t, nil
		case err == nil:
			last = t
		}
		timer.Reset(issuePollInterval)
	}
}

// isTransient reports whether err is worth another poll: a 5xx, a 429 or a
// failure that never reached kupe-api.
func isTransient(err error) bool {
	return client.IsServerError(err) || client.IsRateLimited(err) || !client.IsAPIError(err)
}

// revokeQuietly revokes a token that will never be used (its secret was
// not handed over), even after the command's context ended. It returns the
// failure for a warning.
func revokeQuietly(ctx context.Context, api client.Interface, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return api.DeleteRegistryToken(ctx, client.RegistryName, id)
}

// issueErr explains why a token or session was not handed over; what is
// "access token" or "login session". The token was revoked unless
// revokeErr says otherwise.
func issueErr(err error, what, id string, last *client.RegistryToken, revokeErr error, retry string) error {
	revoked := "it was revoked"
	if revokeErr != nil {
		revoked = fmt.Sprintf("revoking it failed (%v); revoke it with:  kupe registry token revoke %s --yes", revokeErr, id)
	}
	switch {
	case errors.Is(err, errNotIssued):
		return cli.New(cli.ExitUnavailable, fmt.Sprintf("the registry did not issue the %s in time; %s", what, revoked)).WithHint(retry)
	case errors.Is(err, context.Canceled):
		return cli.New(cli.ExitInterrupted, fmt.Sprintf("stopped waiting for the %s; %s", what, revoked))
	case errors.Is(err, errNotUsable):
		msg := fmt.Sprintf("the registry could not issue the %s", what)
		if last != nil && last.Status != nil && last.Status.Message != "" {
			msg += ": " + last.Status.Message
		}
		return cli.New(cli.ExitGeneral, msg+"; "+revoked).WithHint(retry)
	}
	return translateErr(err, "")
}

func newTokenCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "token",
		Aliases: []string{"tokens"},
		Short:   "Manage access tokens for automation",
		Long: `Create, list and revoke access tokens: registry credentials for CI and other
automation, which pull or push (push includes pull) until they expire, 1 to 365
days after creation. A token is revoked when the member who created it leaves
the tenant. Only tenant admins can create push tokens.

The list also shows login sessions from "kupe registry login" (--kind
session); revoke one to sign a machine out early.`,
	}
	cmd.AddCommand(newTokenCreateCmd(f))
	cmd.AddCommand(newTokenListCmd(f))
	cmd.AddCommand(newTokenGetCmd(f))
	cmd.AddCommand(newTokenRevokeCmd(f))
	return cmd
}

// tokenCreateOutput is `token create -o json`: every field of the token,
// its secret and the registry host.
type tokenCreateOutput struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Access    string `json:"access"`
	Username  string `json:"username"`
	Registry  string `json:"registry"`
	Secret    string `json:"secret"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	Phase     string `json:"phase"`
}

type tokenCreateOpts struct {
	name        string
	access      string
	days        int
	wait        bool
	waitTimeout time.Duration
	output      string
}

func newTokenCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &tokenCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create --name NAME --access pull|push",
		Short: "Create an access token",
		Long: `Create an access token and print its secret (kupecr_...) once on stdout. Store
it somewhere safe now: it cannot be shown again. On a TTY the token's ID,
username, access and expiry, and the docker login command, follow on stderr;
otherwise stdout holds only the secret, so TOKEN=$(kupe registry token create
...) works. -o json prints everything, the secret included.

--expires-in-days is 1 to 365 (default 90). By default the command waits
until the platform has issued the token (seconds), so it works as soon as the
command returns; if it is not issued within a minute it is revoked.`,
		Args: cobra.NoArgs,
		Example: `  # Pull token for a deploy pipeline
  TOKEN=$(kupe registry token create --name deploy --access pull)

  # Push token for CI, valid 30 days, everything as JSON
  kupe registry token create --name github-actions --access push --expires-in-days 30 -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := opts.request(cmd)
			if err != nil {
				return err
			}
			switch opts.output {
			case "", "text", "json":
			default:
				return cli.MisuseError(fmt.Sprintf("unsupported output format %q (expected one of: text, json)", opts.output))
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			created, err := api.CreateRegistryToken(cmd.Context(), client.RegistryName, req)
			if err != nil {
				return translateErr(err, "")
			}
			out := tokenCreateOutput{
				ID: created.ID, Name: created.DisplayName, Access: created.Access, Username: created.Username,
				Registry: created.Registry, Secret: created.Secret, CreatedBy: created.CreatedBy,
				CreatedAt: created.CreatedAt, ExpiresAt: created.ExpiresAt, Phase: created.Phase(),
			}
			if opts.wait {
				issued, werr := waitIssued(cmd.Context(), api, created.ID, opts.waitTimeout)
				if werr != nil {
					return issueErr(werr, "access token", created.ID, issued, revokeQuietly(cmd.Context(), api, created.ID),
						"try again:  kupe registry token create")
				}
				out.Username, out.Phase = issued.Username, issued.Phase()
			}
			if opts.output == "json" {
				enc := json.NewEncoder(f.IOStreams.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(out) // #nosec G117 -- -o json prints the secret on stdout: the user asked for it
			}
			return renderTokenCreated(f.IOStreams, &out)
		},
	}
	cmd.Flags().StringVar(&opts.name, "name", "", "A name to recognise the token by, 1 to 128 characters (required)")
	cmd.Flags().StringVar(&opts.access, "access", "", "pull, or push (which includes pull; admins only) (required)")
	cmd.Flags().IntVar(&opts.days, "expires-in-days", 90, "Days until the token stops working, 1 to 365")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait until the platform has issued the token before printing it")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", issueTimeout, "Give up and revoke the token after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpToggle)
	return cmd
}

// request checks the flags and builds the create body; --expires-in-days
// is sent only when given, so the platform's default applies otherwise.
func (o *tokenCreateOpts) request(cmd *cobra.Command) (client.CreateRegistryTokenRequest, error) {
	req := client.CreateRegistryTokenRequest{DisplayName: o.name, Access: o.access}
	switch {
	case strings.TrimSpace(o.name) == "":
		return req, cli.MisuseError("--name is required").WithHint("e.g. --name github-actions")
	case utf8.RuneCountInString(o.name) > 128:
		return req, cli.MisuseError("--name must be 128 characters or less")
	case strings.IndexFunc(o.name, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return req, cli.MisuseError("--name must be printable text")
	}
	switch o.access {
	case client.RegistryAccessPull, client.RegistryAccessPush:
	case "":
		return req, cli.MisuseError("--access is required").WithHint("use --access pull, or --access push (which includes pull)")
	default:
		return req, cli.MisuseError(fmt.Sprintf("invalid --access %q", o.access)).WithHint("use --access pull, or --access push (which includes pull)")
	}
	if cmd.Flags().Changed("expires-in-days") {
		if o.days < minTokenDays || o.days > maxTokenDays {
			return req, cli.MisuseError(fmt.Sprintf("--expires-in-days must be from %d to %d", minTokenDays, maxTokenDays))
		}
		days := o.days
		req.ExpiresInDays = &days
	}
	return req, nil
}

// renderTokenCreated writes the secret alone to stdout and, on a TTY, the
// token's details and the docker login command to stderr. The secret never
// appears in the command: it reads $TOKEN.
func renderTokenCreated(io *cli.IOStreams, t *tokenCreateOutput) error {
	if _, err := fmt.Fprintln(io.Out, t.Secret); err != nil {
		return err
	}
	if !io.IsStderrTTY() {
		return nil
	}
	w := io.ErrOut
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Copy the secret above now: it is shown only once.\n\n")
	fmt.Fprintf(w, "  ID:        %s\n", t.ID)
	fmt.Fprintf(w, "  Username:  %s\n", shellQuote(t.Username))
	fmt.Fprintf(w, "  Access:    %s\n", t.Access)
	fmt.Fprintf(w, "  Expires:   %s\n", t.ExpiresAt)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  To sign Docker in with it, put the secret in TOKEN and run:")
	fmt.Fprintf(w, "    %s\n", dockerLoginLine(t.Registry, t.Username))
	return nil
}

func newTokenListCmd(f *cli.Factory) *cobra.Command {
	var output, kind string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List access tokens and login sessions",
		Long: `List the registry's access tokens and login sessions, newest first. Secrets
are never shown. --kind token or --kind session lists one kind.`,
		Args: cobra.NoArgs,
		Example: `  kupe registry token list
  kupe registry token list --kind session -o wide`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch kind {
			case "all":
				kind = ""
			case client.RegistryTokenKindToken, client.RegistryTokenKindSession:
			default:
				return cli.MisuseError(fmt.Sprintf("invalid --kind %q", kind)).WithHint("use --kind token, session or all")
			}
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListRegistryTokens(cmd.Context(), client.RegistryName, kind)
			if err != nil {
				return translateErr(err, "")
			}
			return printer.RegistryToken.List(f.IOStreams, format, list)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "all", "Which to list: token (access tokens) | session (login sessions) | all")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newTokenGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "get ID",
		Short:   "Show an access token or login session",
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry token get rt-0123456789abcdef`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			t, err := api.GetRegistryToken(cmd.Context(), client.RegistryName, args[0])
			if err != nil {
				return translateErr(err, "")
			}
			return printer.RegistryToken.One(f.IOStreams, format, t)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newTokenRevokeCmd(f *cli.Factory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "revoke ID",
		Short: "Revoke an access token or login session",
		Long: `Revoke an access token or login session. Admins can revoke any; a read-only
member can revoke only their own login sessions.

Prompts on a TTY; pass --yes to skip (required in non-interactive
environments).`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry token revoke rt-0123456789abcdef --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			io := f.IOStreams
			if err := cli.ConfirmAction(io, yes, "revoke", fmt.Sprintf("This will revoke access token or login session %q.", id)); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteRegistryToken(cmd.Context(), client.RegistryName, id); err != nil {
				return translateErr(err, "")
			}
			fmt.Fprintf(io.Out, "token/%s revoked\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
