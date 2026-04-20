package auth

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// whoamiFactory wires a Factory whose Client() returns a pre-filled Fake.
// Config is seeded so Resolved/Context lookups succeed without touching the
// real filesystem.
func whoamiFactory(t *testing.T, fake *clienttest.Fake) (*cli.Factory, *cli.IOStreams) {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://api.test", Tenant: "acme", TokenRef: "keyring", User: "billy@acme.com"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f, io
}

func TestWhoamiTextOutput(t *testing.T) {
	fake := clienttest.New()
	fake.TenantName = "acme"
	fake.TenantResponse = &client.Tenant{Name: "acme", DisplayName: "Acme Corp", Plan: "starter"}

	f, io := whoamiFactory(t, fake)
	cmd := newWhoamiCmd(f)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("whoami: %v", err)
	}

	out := io.Out.(interface{ String() string }).String()
	for _, want := range []string{"billy@acme.com", "Acme Corp (acme)", "starter", "https://api.test", "keyring"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWhoamiJSONOutput(t *testing.T) {
	fake := clienttest.New()
	fake.TenantResponse = &client.Tenant{Name: "acme", DisplayName: "Acme Corp", Plan: "starter"}

	f, io := whoamiFactory(t, fake)
	cmd := newWhoamiCmd(f)
	cmd.SetArgs([]string{"-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("whoami -o json: %v", err)
	}

	var got whoamiOutput
	if err := json.Unmarshal([]byte(io.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.User != "billy@acme.com" || got.Tenant != "acme" || got.Plan != "starter" {
		t.Fatalf("unexpected JSON payload: %+v", got)
	}
}

func TestWhoamiSurfacesAPIError(t *testing.T) {
	fake := clienttest.New()
	fake.TenantErr = &client.APIError{StatusCode: 401, Message: "bad token", RequestID: "req-x"}

	f, _ := whoamiFactory(t, fake)
	cmd := newWhoamiCmd(f)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("wrong error kind: %v", err)
	}
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("ExitCode = %d; want %d", code, cli.ExitAuth)
	}
}
