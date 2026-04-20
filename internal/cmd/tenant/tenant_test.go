package tenant

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

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

func run(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestGetRendersDetailView(t *testing.T) {
	fake := clienttest.New()
	fake.TenantResponse = &client.Tenant{
		Name: "acme", DisplayName: "Acme Corp", Plan: "pro",
		Status: &client.TenantStatus{
			Phase:         "Active",
			ClusterCount:  3,
			PoolResources: &client.ResourcePool{CPU: "8", Memory: "32", Storage: "200"},
			CurrentUsage: &client.TenantCurrentUsage{
				EstimatedTotal: "156.15",
				Currency:       "GBP",
			},
		},
		Members: []client.Member{
			{Email: "alice@acme.com", Role: "admin"},
		},
	}

	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "get"); err != nil {
		t.Fatalf("tenant get: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, want := range []string{"acme", "Acme Corp", "pro", "Active", "CPU=8 MEM=32 STORAGE=200", "156.15 GBP", "Members:"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
}

func TestGetJSONPreservesNestedUsage(t *testing.T) {
	fake := clienttest.New()
	fake.TenantResponse = &client.Tenant{
		Name: "acme",
		Status: &client.TenantStatus{
			Phase: "Active",
			CurrentUsage: &client.TenantCurrentUsage{
				EstimatedTotal: "156.15",
				Compute: &client.TenantCurrentUsageCompute{
					CPUCoreHours: "2976.00",
					CPUCost:      "89.28",
				},
			},
		},
	}

	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "get", "-o", "json"); err != nil {
		t.Fatalf("tenant get -o json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatalf("json invalid: %v", err)
	}
	status, _ := got["status"].(map[string]any)
	usage, _ := status["currentUsage"].(map[string]any)
	compute, _ := usage["compute"].(map[string]any)
	if compute["cpuCost"] != "89.28" {
		t.Fatalf("nested compute.cpuCost lost in JSON: %+v", compute)
	}
}

func TestGetPreferencesOutputHonoured(t *testing.T) {
	fake := clienttest.New()
	fake.TenantResponse = &client.Tenant{Name: "acme"}

	// Seed the config with preferences.output = "name" so that running
	// "kupe tenant get" with NO -o flag emits the name-only line.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://t", Tenant: "acme", TokenRef: "plaintext"},
		},
		Preferences: config.Preferences{Output: "name"},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.Client = func() (client.Interface, error) { return fake, nil }

	if err := run(NewCmd(f), "get"); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(f.IOStreams.Out.(interface{ String() string }).String())
	if out != "acme" {
		t.Fatalf("preferences.output=name not honoured; got %q", out)
	}
}
