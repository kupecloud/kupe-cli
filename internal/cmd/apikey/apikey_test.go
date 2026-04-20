package apikey

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// factoryWith wires a Factory whose Client returns the given Fake. Mirrors
// the helper in cluster_test.go — duplicated here to keep the package
// boundary clean.
func factoryWith(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

func executeCmd(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestListEmptyRendersHeaderOnly(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	if err := executeCmd(newListCmd(f)); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	if !strings.Contains(out, "NAME") {
		t.Fatalf("missing header:\n%s", out)
	}
}

func TestListTableAndJSON(t *testing.T) {
	fake := clienttest.New()
	fake.APIKeys["abc12345-..."] = &client.APIKey{
		ID:          "abc12345-aaaa-bbbb-cccc-000000000000",
		DisplayName: "CI",
		Role:        "admin",
		CreatedBy:   "billy@acme.com",
		CreatedAt:   "2026-04-20T00:00:00Z",
	}

	// Table
	f := factoryWith(t, fake)
	if err := executeCmd(newListCmd(f)); err != nil {
		t.Fatal(err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, s := range []string{"CI", "admin", "billy@acme.com", "abc12345"} {
		if !strings.Contains(out, s) {
			t.Errorf("table missing %q:\n%s", s, out)
		}
	}

	// JSON
	f = factoryWith(t, fake)
	if err := executeCmd(newListCmd(f), "-o", "json"); err != nil {
		t.Fatal(err)
	}
	var got []client.APIKey
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(got) != 1 || got[0].DisplayName != "CI" {
		t.Fatalf("unexpected json: %+v", got)
	}
	// Never leak Key on list.
	if got[0].Key != "" {
		t.Fatalf("list response leaked raw key: %q", got[0].Key)
	}
}

func TestCreateStdoutOnlyHasToken(t *testing.T) {
	fake := clienttest.New()
	fake.NextAPIKeyID = "cafe1234"

	f := factoryWith(t, fake)
	// Force stderr to non-TTY so the metadata block is suppressed.
	f.IOStreams.SetSpinnersEnabled(false)

	if err := executeCmd(newCreateCmd(f), "--name", "CI", "--role", "admin"); err != nil {
		t.Fatalf("create: %v", err)
	}
	stdout := f.IOStreams.Out.(interface{ String() string }).String()
	stderr := f.IOStreams.ErrOut.(interface{ String() string }).String()

	if strings.TrimSpace(stdout) != "kupe_cafe1234_fakesecret" {
		t.Fatalf("stdout should be the token only, got:\n%q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty on non-TTY, got: %q", stderr)
	}
}

func TestCreateJSONIncludesToken(t *testing.T) {
	fake := clienttest.New()
	fake.NextAPIKeyID = "cafe1234"
	f := factoryWith(t, fake)

	if err := executeCmd(newCreateCmd(f), "--name", "CI", "--role", "readonly", "-o", "json"); err != nil {
		t.Fatalf("create -o json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got["token"] != "kupe_cafe1234_fakesecret" {
		t.Fatalf(".token wrong: %v", got["token"])
	}
	if got["name"] != "CI" || got["role"] != "readonly" {
		t.Fatalf("metadata fields wrong: %+v", got)
	}
}

func TestCreateForbiddenMapsToExit3(t *testing.T) {
	fake := clienttest.New()
	fake.CreateAPIKeyErr = &client.APIError{StatusCode: 403, Message: "admin access required"}

	f := factoryWith(t, fake)
	err := executeCmd(newCreateCmd(f), "--name", "x", "--role", "admin")
	if err == nil {
		t.Fatal("expected 403 error")
	}
	if code := cli.ExitCode(err); code != cli.ExitAuth {
		t.Fatalf("exit = %d; want %d", code, cli.ExitAuth)
	}
}

func TestCreateRejectsInvalidRole(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := executeCmd(newCreateCmd(f), "--name", "x", "--role", "superuser")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
}

func TestDeleteNonInteractiveRequiresYes(t *testing.T) {
	fake := clienttest.New()
	fake.APIKeys["id1"] = &client.APIKey{ID: "id1", DisplayName: "x", Role: "admin"}

	f := factoryWith(t, fake)
	err := executeCmd(newDeleteCmd(f), "id1")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
	if _, still := fake.APIKeys["id1"]; !still {
		t.Fatal("apikey revoked despite CI refusal")
	}
}

func TestDeleteYesRemovesKey(t *testing.T) {
	fake := clienttest.New()
	fake.APIKeys["id1"] = &client.APIKey{ID: "id1", DisplayName: "x", Role: "admin"}

	f := factoryWith(t, fake)
	if err := executeCmd(newDeleteCmd(f), "id1", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, still := fake.APIKeys["id1"]; still {
		t.Fatal("apikey still present after delete --yes")
	}
}

func TestDeleteNotFoundMapsToExit4(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := executeCmd(newDeleteCmd(f), "ghost", "--yes")
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}

func TestResolveExpiresAt(t *testing.T) {
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"7d", "2026-04-27T00:00:00Z"},
		{"24h", "2026-04-21T00:00:00Z"},
		{"2026-12-31T23:59:59Z", "2026-12-31T23:59:59Z"},
	}
	for _, tt := range tests {
		got, err := resolveExpiresAt(tt.in, now)
		if err != nil {
			t.Errorf("resolveExpiresAt(%q) err: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("resolveExpiresAt(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}

	if _, err := resolveExpiresAt("garbage", now); err == nil {
		t.Error("want error for garbage duration")
	}
}
