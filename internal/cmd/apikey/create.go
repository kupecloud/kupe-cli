package apikey

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// createResponse is the v1 JSON schema for `kupe apikey create -o json`.
// Its `token` field is the raw kupe_... value — the field is called `key`
// on the server but `token` matches how users think about the value.
type createResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Token     string `json:"token"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type createOpts struct {
	name      string
	role      string
	expiresAt string
	output    string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{role: client.RoleReadonly}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Mint a new API key",
		Long: `Create a new API key. The raw kupe_... token is printed ONCE on stdout —
store it somewhere safe; it cannot be retrieved again.

On a TTY, key metadata (ID, role, expiry) is additionally printed on
stderr. In non-interactive mode (pipe, CI), only the raw token appears on
stdout so "TOKEN=$(kupe apikey create ...)" works cleanly.

--expires-at accepts a relative duration (` + "`7d`, `30d`, `90d`, `24h`" + `)
or an absolute RFC3339 timestamp. Omit for no expiry.`,
		Example: `  # Interactive — metadata to stderr, token to stdout
  kupe apikey create --name "CI Pipeline" --role admin --expires-at 90d

  # Scripted — capture just the token
  TOKEN=$(kupe apikey create --name ci-$CI_COMMIT --role admin --expires-at 7d)

  # Full JSON including metadata
  kupe apikey create --name audit --role readonly -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCreate(cmd, f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "Human-readable display name (required)")
	cmd.Flags().StringVar(&opts.role, "role", client.RoleReadonly, "Role: admin or readonly")
	cmd.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Expiration: duration (7d, 24h) or RFC3339 (default: never)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format: text (default) or json")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runCreate(cmd *cobra.Command, f *cli.Factory, opts *createOpts) error {
	switch opts.role {
	case client.RoleAdmin, client.RoleReadonly:
	default:
		return cli.MisuseError(fmt.Sprintf("invalid --role %q (want admin or readonly)", opts.role))
	}

	expires, err := resolveExpiresAt(opts.expiresAt, time.Now())
	if err != nil {
		return cli.MisuseError(err.Error())
	}

	api, err := f.Client()
	if err != nil {
		return err
	}
	key, err := api.CreateAPIKey(cmd.Context(), client.CreateAPIKeyRequest{
		DisplayName: opts.name,
		Role:        opts.role,
		ExpiresAt:   expires,
	})
	if err != nil {
		return err
	}

	switch opts.output {
	case "", "text":
		return renderCreateText(f, key)
	case "json":
		enc := json.NewEncoder(f.IOStreams.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(createResponseFrom(key))
	default:
		return cli.MisuseError(fmt.Sprintf("unsupported output format %q (expected one of: text, json)", opts.output))
	}
}

// renderCreateText writes the raw token to stdout and, on a TTY, the
// human-friendly metadata block to stderr. The contract is:
//
//   - stdout is always just the raw key + newline, so TOKEN=$(…) works.
//   - stderr carries the "store this securely" notice and a few summary
//     lines, TTY-only (would pollute CI logs otherwise).
func renderCreateText(f *cli.Factory, key *client.APIKey) error {
	if _, err := fmt.Fprintln(f.IOStreams.Out, key.Key); err != nil {
		return err
	}
	if f.IOStreams.IsStderrTTY() {
		fmt.Fprintln(f.IOStreams.ErrOut)
		fmt.Fprintf(f.IOStreams.ErrOut, "  Copy the token above now — it is shown only once.\n\n")
		fmt.Fprintf(f.IOStreams.ErrOut, "  ID:      %s\n", key.ID)
		fmt.Fprintf(f.IOStreams.ErrOut, "  Name:    %s\n", key.DisplayName)
		fmt.Fprintf(f.IOStreams.ErrOut, "  Role:    %s\n", key.Role)
		if key.ExpiresAt != "" {
			fmt.Fprintf(f.IOStreams.ErrOut, "  Expires: %s\n", key.ExpiresAt)
		} else {
			fmt.Fprintf(f.IOStreams.ErrOut, "  Expires: never\n")
		}
	}
	return nil
}

func createResponseFrom(key *client.APIKey) createResponse {
	return createResponse{
		ID:        key.ID,
		Name:      key.DisplayName,
		Role:      key.Role,
		Token:     key.Key,
		CreatedAt: key.CreatedAt,
		ExpiresAt: key.ExpiresAt,
	}
}

// resolveExpiresAt turns a user-friendly --expires-at value into the
// RFC3339 UTC timestamp the API expects. Accepted forms:
//
//	""             → "" (no expiry)
//	"30d", "7d"    → parsed as days, added to now
//	"24h", "90m"   → time.ParseDuration (hours / minutes / seconds)
//	RFC3339 date   → returned as-is after a shape check
func resolveExpiresAt(raw string, now time.Time) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasSuffix(raw, "d") {
		if days, err := strconv.Atoi(strings.TrimSuffix(raw, "d")); err == nil {
			return now.AddDate(0, 0, days).UTC().Format(time.RFC3339), nil
		}
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return now.Add(d).UTC().Format(time.RFC3339), nil
	}
	if _, err := time.Parse(time.RFC3339, raw); err == nil {
		return raw, nil
	}
	return "", fmt.Errorf("invalid --expires-at %q (want duration like 7d/24h or RFC3339)", raw)
}
