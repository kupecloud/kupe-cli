package cmd

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// TestPrintErrorHintTenantAccessDenied pins kupe-cli#73: a tenant_access_denied
// refusal prints a hint naming where the tenant came from and how to switch.
func TestPrintErrorHintTenantAccessDenied(t *testing.T) {
	denied := &client.APIError{StatusCode: 403, Code: "tenant_access_denied", Message: `access denied to tenant "aware"`}
	const login = "  or log in to the tenant you want: kupe auth login --tenant NAME\n"

	tests := []struct {
		name      string
		err       error
		contexts  []string // "" config means no config file at all
		current   string
		flags     cli.GlobalFlags
		envTenant string
		envCtx    string
		want      string
	}{
		{
			name:     "current context named, others listed sorted",
			err:      denied,
			contexts: []string{"aware", "hello-kupe", "demo", "acme-corp"},
			current:  "aware",
			want: "  current context: aware\n" +
				"  switch with: kupe config use-context NAME (your contexts: acme-corp, demo, hello-kupe)\n" + login,
		},
		{
			name:     "--context overrides the current context",
			err:      denied,
			contexts: []string{"aware", "demo"},
			current:  "demo",
			flags:    cli.GlobalFlags{Context: "aware"},
			want: "  current context: aware\n" +
				"  switch with: kupe config use-context NAME (your contexts: demo)\n" + login,
		},
		{
			name:     "KUPE_CONTEXT overrides the current context",
			err:      denied,
			contexts: []string{"aware", "demo"},
			current:  "demo",
			envCtx:   "aware",
			want: "  current context: aware\n" +
				"  switch with: kupe config use-context NAME (your contexts: demo)\n" + login,
		},
		{
			name:     "only context: no switch line",
			err:      denied,
			contexts: []string{"aware"},
			current:  "aware",
			want:     "  current context: aware\n" + login,
		},
		{
			name:     "--tenant picked the tenant",
			err:      denied,
			contexts: []string{"aware", "demo"},
			current:  "demo",
			flags:    cli.GlobalFlags{Tenant: "aware"},
			want:     "  the tenant comes from --tenant; check the name\n" + login,
		},
		{
			name:      "KUPE_TENANT picked the tenant",
			err:       denied,
			contexts:  []string{"aware", "demo"},
			current:   "demo",
			envTenant: "aware",
			want:      "  the tenant comes from KUPE_TENANT; check or unset it\n" + login,
		},
		{
			name: "no config file",
			err:  denied,
			want: login,
		},
		{
			name:     "plain 403 without the code gets no hint",
			err:      &client.APIError{StatusCode: 403, Message: "access denied"},
			contexts: []string{"aware"},
			current:  "aware",
		},
		{
			name:     "a command's own hint wins",
			err:      cli.WrapAPI(cli.ExitAuth, "refused", denied).WithHint("check the tenant name and the token's role"),
			contexts: []string{"aware", "demo"},
			current:  "aware",
			want:     "  check the tenant name and the token's role\n",
		},
		{
			name:     "code survives wrapping",
			err:      fmt.Errorf("creating session: %w", denied),
			contexts: []string{"aware"},
			current:  "aware",
			want:     "  current context: aware\n" + login,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUPE_TENANT", tt.envTenant)
			t.Setenv("KUPE_CONTEXT", tt.envCtx)
			t.Setenv("KUPE_CONFIG", "")
			flags := tt.flags
			flags.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
			if tt.contexts != nil {
				cfg := &config.Config{CurrentContext: tt.current}
				for _, name := range tt.contexts {
					cfg.SetContext(config.Context{Name: name, Tenant: name})
				}
				if err := cfg.Save(flags.ConfigPath); err != nil {
					t.Fatal(err)
				}
			}
			io, _, errOut := cli.Test()

			printErrorHint(io, &flags, tt.err)

			if got := errOut.String(); got != tt.want {
				t.Errorf("hint:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
