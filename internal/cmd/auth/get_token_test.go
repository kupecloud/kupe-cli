package auth

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/auth"
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/config"
)

func TestGetTokenEmitsValidExecCredential(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Seed config + plaintext credentials so f.Token() resolves.
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "prod",
		Contexts: []config.Context{
			{Name: "prod", APIURL: "https://api.test", Tenant: "acme", TokenRef: "plaintext"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUPE_STORAGE", "plaintext")

	credsPath := auth.DefaultCredentialsPath(cfgPath)
	mgr := auth.NewManager(credsPath)
	if _, err := mgr.Set("prod", "kupe_exec_test"); err != nil {
		t.Fatal(err)
	}

	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.Auth = func() (*auth.Manager, error) { return auth.NewManager(credsPath), nil }

	cmd := newGetTokenCmd(f)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--context=prod"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("get-token: %v", err)
	}

	out := io.Out.(interface{ String() string }).String()

	// Parse as the raw shape kubectl expects.
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("ExecCredential not valid JSON: %v\n%s", err, out)
	}
	if got["apiVersion"] != "client.authentication.k8s.io/v1" {
		t.Errorf("apiVersion = %v", got["apiVersion"])
	}
	if got["kind"] != "ExecCredential" {
		t.Errorf("kind = %v", got["kind"])
	}
	status, ok := got["status"].(map[string]any)
	if !ok {
		t.Fatalf("status missing: %v", got)
	}
	if status["token"] != "kupe_exec_test" {
		t.Errorf("status.token = %v", status["token"])
	}
}

func TestGetTokenExitsAuthErrorWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Config exists but tokenRef is empty — logged-out state.
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "prod",
		Contexts: []config.Context{
			{Name: "prod", APIURL: "https://api.test", Tenant: "acme"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUPE_API_TOKEN", "")

	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)

	cmd := newGetTokenCmd(f)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--context=prod"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d", code, cli.ExitAuth)
	}
}

// Sanity: the command is registered under `auth` and hidden from help.
func TestGetTokenIsHidden(t *testing.T) {
	io, _, _ := cli.Test()
	f := cli.NewFactory(io, &cli.GlobalFlags{})
	cmd := newGetTokenCmd(f)
	if !cmd.Hidden {
		t.Fatal("get-token should be hidden from default help")
	}
}

// compile-time: the returned command accepts a Context from SetContext.
var _ = func() *cobra.Command { return (&cobra.Command{}) }
