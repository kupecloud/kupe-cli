package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/dockerconfig"
)

// plainStorageWarning is shown when a credential is saved in the Docker
// config file itself, with no credential helper to keep it in a keychain.
const plainStorageWarning = "Your Docker credential was saved unencrypted in %s. Configure a credential helper " +
	"(https://docs.docker.com/reference/cli/docker/login/#credential-stores) to keep it in your system keychain.\n"

// dockerUnauthorized is the word Docker prints once a login has expired.
const dockerUnauthorized = "unauthorized" //nolint:misspell // Docker's own error text, quoted so people recognise it

// loginOutput is `login --no-store -o json`: what another client (Podman,
// Buildah, a CI wrapper) needs to sign in.
type loginOutput struct {
	Server    string `json:"server"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	ExpiresAt string `json:"expiresAt"`
}

type loginOpts struct {
	dockerConfig string
	noStore      bool
	output       string
}

func newLoginCmd(f *cli.Factory) *cobra.Command {
	opts := &loginOpts{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign Docker in to the registry for 12 hours",
		Long: `Sign Docker in to your registry with a login session that lasts 12 hours.
Admins can push and pull; read-only members can pull. Run it again after the
session expires: Docker then reports "` + dockerUnauthorized + `".

The credential is stored as "docker login" would store it: through the
credential helper your Docker config names for the registry (credHelpers,
then credsStore), or, with none, in the config file itself with a warning.
The config is DOCKER_CONFIG/config.json, or ~/.docker/config.json;
--docker-config DIR uses DIR/config.json.

Login sessions are for people: sign in with "kupe auth login" first. An API
key is refused; for automation, create an access token with
"kupe registry token create". --no-store -o json prints the server, username,
password and expiry as JSON on stdout and stores nothing, for Podman, Buildah
or another client.`,
		Args: cobra.NoArgs,
		Example: `  kupe registry login
  docker push registry.kupe.cloud/acme/team-a/api:v1

  # Podman
  creds=$(kupe registry login --no-store -o json)
  jq -r .password <<<"$creds" | podman login registry.kupe.cloud --username "$(jq -r .username <<<"$creds")" --password-stdin`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd.Context(), f, opts)
		},
	}
	cmd.Flags().StringVar(&opts.dockerConfig, "docker-config", "", "Directory of the Docker config to store the credential in (as docker --config)")
	cmd.Flags().BoolVar(&opts.noStore, "no-store", false, "Store nothing; print the credential with -o json")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: json (only with --no-store)")
	return cmd
}

func runLogin(ctx context.Context, f *cli.Factory, opts *loginOpts) error {
	switch {
	case opts.output != "" && opts.output != "json":
		return cli.MisuseError(fmt.Sprintf("unsupported output format %q (login prints only json, with --no-store)", opts.output))
	case opts.noStore && opts.output != "json":
		return cli.MisuseError("--no-store needs -o json: the credential has to go somewhere")
	case !opts.noStore && opts.output == "json":
		return cli.MisuseError("-o json prints the password, so it needs --no-store")
	}
	// The Docker config is read, and its helper checked, before a session is
	// asked for, so a session is never minted that could not be stored. The
	// helper is the one for the registry's own host, read from the registry
	// as logout does (CR-4e F-2).
	var cfg *dockerconfig.File
	if !opts.noStore {
		var err error
		if cfg, err = loadDockerConfig(opts.dockerConfig); err != nil {
			return err
		}
	}
	api, err := f.Client()
	if err != nil {
		return err
	}
	if cfg != nil {
		checkHost := registryHost(ctx, api)
		helper, err := cfg.HelperFor(checkHost)
		if err != nil {
			return cli.Wrap(cli.ExitGeneral, "reading the Docker config", err)
		}
		if helper.Name != "" {
			if err := helper.Check(); err != nil {
				return helperErr(err)
			}
		}
	}
	session, err := api.CreateRegistrySession(ctx, client.RegistryName)
	if err != nil {
		return translateErr(err, "")
	}
	issued, err := waitIssued(ctx, api, session.ID, issueTimeout)
	if err != nil {
		return issueErr(err, "login session", session.ID, issued, revokeQuietly(ctx, api, session.ID),
			"try again:  kupe registry login")
	}
	host := session.Registry
	if host == "" {
		host = client.DefaultRegistryHost
	}
	io := f.IOStreams
	if opts.noStore {
		enc := json.NewEncoder(io.Out)
		enc.SetIndent("", "  ")
		// #nosec G117 -- --no-store -o json prints the password on stdout: the user asked for it
		return enc.Encode(loginOutput{Server: host, Username: issued.Username, Password: session.Secret, ExpiresAt: session.ExpiresAt})
	}
	stored, err := cfg.Store(ctx, dockerconfig.Credential{ServerURL: host, Username: issued.Username, Secret: session.Secret})
	if err != nil {
		if rerr := revokeQuietly(ctx, api, session.ID); rerr != nil {
			fmt.Fprintf(io.ErrOut, "Warning: the login session %s could not be revoked: %v\n", session.ID, rerr)
		}
		return helperErr(err)
	}
	if stored.Helper.Name == "" {
		fmt.Fprintf(io.ErrOut, plainStorageWarning, stored.Path)
	}
	fmt.Fprintf(io.ErrOut, "Logged in to %s as %s until %s. Admins can push and pull; read-only members can pull. "+
		"Run \"kupe registry login\" again after it expires: Docker reports %q once it has.\n",
		host, shellQuote(issued.Username), untilText(session.CreatedAt, session.ExpiresAt), dockerUnauthorized)
	return nil
}

// loadDockerConfig resolves and reads the Docker config.
func loadDockerConfig(dir string) (*dockerconfig.File, error) {
	path, err := dockerconfig.Path(dir)
	if err != nil {
		return nil, cli.Wrap(cli.ExitGeneral, "finding the Docker config", err)
	}
	cfg, err := dockerconfig.Load(path)
	if err != nil {
		return nil, cli.Wrap(cli.ExitGeneral, "", err)
	}
	return cfg, nil
}

// helperErr maps a credential store failure: an invalid helper name is the
// config's misuse (exit 2), anything else a general failure naming it.
func helperErr(err error) error {
	if errors.Is(err, dockerconfig.ErrInvalidHelper) {
		return cli.Wrap(cli.ExitMisuse, "", err)
	}
	return cli.Wrap(cli.ExitGeneral, "", err)
}

// untilText is the expiry in local time with the session's length: "2026-10-22
// 22:00 BST (12 hours)". It falls back to the raw value when it cannot be
// read.
func untilText(createdAt, expiresAt string) string {
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return expiresAt
	}
	out := expires.Local().Format("2006-01-02 15:04 MST")
	if created, err := time.Parse(time.RFC3339, createdAt); err == nil {
		if hours := int(math.Round(expires.Sub(created).Hours())); hours > 0 {
			unit := "hours"
			if hours == 1 {
				unit = "hour"
			}
			out += fmt.Sprintf(" (%d %s)", hours, unit)
		}
	}
	return out
}

type logoutOpts struct {
	dockerConfig string
}

func newLogoutCmd(f *cli.Factory) *cobra.Command {
	opts := &logoutOpts{}
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove the registry credential from Docker and end your login sessions",
		Long: `Remove the registry credential from Docker, as "docker logout" would (from the
credential helper and from the config file), and revoke your own live login
sessions, on every machine you ran "kupe registry login" on. Access tokens are
not touched. A session that cannot be revoked is reported as a warning; it
still expires 12 hours after it was made.`,
		Args:    cobra.NoArgs,
		Example: `  kupe registry logout`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.Context(), f, opts)
		},
	}
	cmd.Flags().StringVar(&opts.dockerConfig, "docker-config", "", "Directory of the Docker config to remove the credential from (as docker --config)")
	return cmd
}

func runLogout(ctx context.Context, f *cli.Factory, opts *logoutOpts) error {
	io := f.IOStreams
	cfg, err := loadDockerConfig(opts.dockerConfig)
	if err != nil {
		return err
	}
	api, err := f.Client()
	if err != nil {
		return err
	}
	host := registryHost(ctx, api)
	erased, err := cfg.Erase(ctx, host)
	if err != nil {
		return helperErr(err)
	}
	if erased.Found {
		fmt.Fprintf(io.ErrOut, "Removed the Docker credential for %s.\n", host)
	} else {
		fmt.Fprintf(io.ErrOut, "Docker had no credential stored for %s.\n", host)
	}
	revokeOwnSessions(ctx, f, api)
	return nil
}

// registryHost is the registry's host from its endpoint, or the default
// host when the registry cannot be read or reports none yet.
func registryHost(ctx context.Context, api client.Interface) string {
	if reg, _, err := api.Registries().Get(ctx, client.RegistryName); err == nil && reg.Status.Host() != "" {
		return reg.Status.Host()
	}
	return client.DefaultRegistryHost
}

// revokeOwnSessions revokes the caller's live login sessions; each failure
// is a warning, not an error.
func revokeOwnSessions(ctx context.Context, f *cli.Factory, api client.Interface) {
	io := f.IOStreams
	user := callerEmail(f)
	if user == "" {
		fmt.Fprintln(io.ErrOut, "Login sessions were not revoked: this context has no signed-in user (sign in with \"kupe auth login\").")
		return
	}
	sessions, err := api.ListRegistryTokens(ctx, client.RegistryName, client.RegistryTokenKindSession)
	if err != nil {
		fmt.Fprintf(io.ErrOut, "Warning: could not list your login sessions to revoke them: %v\n", err)
		return
	}
	revoked := 0
	for _, s := range sessions {
		if !strings.EqualFold(s.CreatedBy, user) {
			continue
		}
		if err := api.DeleteRegistryToken(ctx, client.RegistryName, s.ID); err != nil {
			fmt.Fprintf(io.ErrOut, "Warning: could not revoke login session %s: %v\n", s.ID, err)
			continue
		}
		revoked++
	}
	switch revoked {
	case 0:
		fmt.Fprintln(io.ErrOut, "You had no live login sessions.")
	case 1:
		fmt.Fprintln(io.ErrOut, "Revoked 1 login session.")
	default:
		fmt.Fprintf(io.ErrOut, "Revoked %d login sessions.\n", revoked)
	}
}

// callerEmail is the signed-in user of the current context, "" when it has
// none (an API key, or a token from a flag or the environment).
func callerEmail(f *cli.Factory) string {
	cfg, err := f.Config()
	if err != nil {
		return ""
	}
	resolved, err := f.Resolved()
	if err != nil {
		return ""
	}
	if ctx := cfg.Context(resolved.ContextName); ctx != nil {
		return ctx.User
	}
	return ""
}
