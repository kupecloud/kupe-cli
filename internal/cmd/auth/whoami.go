package auth

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

type whoamiOutput struct {
	User              string `json:"user,omitempty"`
	Tenant            string `json:"tenant"`
	TenantDisplayName string `json:"tenantDisplayName,omitempty"`
	Plan              string `json:"plan,omitempty"`
	Context           string `json:"context,omitempty"`
	APIURL            string `json:"apiUrl"`
	Storage           string `json:"storage,omitempty"`
}

func newWhoamiCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the authenticated tenant and context",
		Long: `Contact kupe-api to confirm the current credentials work and render the
resolved identity. Exits 3 if the token is missing or rejected, 4 if the
tenant does not exist.

Role information is not yet surfaced — it lands alongside the apikey
commands in a later phase.`,
		Example: `  kupe auth whoami
  kupe auth whoami -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWhoami(cmd, f, output)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: text (default) or json")
	return cmd
}

func runWhoami(cmd *cobra.Command, f *cli.Factory, output string) error {
	api, err := f.Client()
	if err != nil {
		return err
	}

	t, _, err := api.GetTenant(cmd.Context())
	if err != nil {
		return err
	}

	out := whoamiOutput{
		Tenant:            t.Name,
		TenantDisplayName: t.DisplayName,
		Plan:              t.Plan,
	}
	out.User = lookupUserFromConfig(f, &out) // side-effect: fills Context, APIURL, Storage

	switch output {
	case "json":
		enc := json.NewEncoder(f.IOStreams.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	case "", "text":
		return renderWhoamiText(f, &out)
	default:
		return cli.MisuseError(fmt.Sprintf("unsupported output format %q (expected one of: text, json)", output))
	}
}

// lookupUserFromConfig fills in the locally-known fields (context name, API
// URL, token storage, and cached user) from the config without re-issuing
// a network call.
func lookupUserFromConfig(f *cli.Factory, out *whoamiOutput) string {
	cfg, err := f.Config()
	if err != nil {
		return ""
	}
	resolved, err := f.Resolved()
	if err != nil {
		return ""
	}
	out.Context = resolved.ContextName
	out.APIURL = resolved.APIURL

	if ctx := cfg.Context(resolved.ContextName); ctx != nil {
		out.Storage = ctx.TokenRef
		return ctx.User
	}
	return ""
}

func renderWhoamiText(f *cli.Factory, o *whoamiOutput) error {
	user := o.User
	if user == "" {
		user = "(not set)"
	}
	storage := o.Storage
	if storage == "" {
		storage = "(env var / flag)"
	}
	tenantLine := o.Tenant
	if o.TenantDisplayName != "" {
		tenantLine = fmt.Sprintf("%s (%s)", o.TenantDisplayName, o.Tenant)
	}
	ctx := o.Context
	if ctx == "" {
		ctx = "(unset)"
	}

	fmt.Fprintf(f.IOStreams.Out, "User:    %s\n", user)
	fmt.Fprintf(f.IOStreams.Out, "Tenant:  %s\n", tenantLine)
	if o.Plan != "" {
		fmt.Fprintf(f.IOStreams.Out, "Plan:    %s\n", o.Plan)
	}
	fmt.Fprintf(f.IOStreams.Out, "Context: %s (%s)\n", ctx, o.APIURL)
	fmt.Fprintf(f.IOStreams.Out, "Storage: %s\n", storage)
	return nil
}
