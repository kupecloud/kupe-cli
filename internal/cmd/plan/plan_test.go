package plan

import (
	"context"
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
	f.PublicClient = func() (client.Interface, error) { return fake, nil }
	return f
}

func run(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestPlanList(t *testing.T) {
	fake := clienttest.New()
	fake.Plans["starter"] = &client.Plan{Name: "starter", DisplayName: "Starter", MaxClusters: 2, PlatformFee: "0.00"}
	fake.Plans["pro"] = &client.Plan{
		Name: "pro", DisplayName: "Pro", MaxClusters: 5, PlatformFee: "49.00",
		ResourcePool:      &client.ResourcePool{CPU: "8", Memory: "32", Storage: "200"},
		ObservabilityPool: &client.PlanObservabilityPool{MaxActiveSeries: 50000, LogIngestGB: 50, RetentionDays: 90, MaxReceivers: 10},
	}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "list"); err != nil {
		t.Fatal(err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, want := range []string{"starter", "pro", "49.00", "CPU=8 MEM=32 STORAGE=200"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
}

func TestPlanListNameMode(t *testing.T) {
	fake := clienttest.New()
	fake.Plans["starter"] = &client.Plan{Name: "starter"}
	fake.Plans["pro"] = &client.Plan{Name: "pro"}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "list", "-o", "name"); err != nil {
		t.Fatal(err)
	}
	names := strings.Fields(strings.TrimSpace(f.IOStreams.Out.(interface{ String() string }).String()))
	if len(names) != 2 {
		t.Fatalf("want 2 names, got %v", names)
	}
}

func TestPlanGetDetailView(t *testing.T) {
	fake := clienttest.New()
	fake.Plans["pro"] = &client.Plan{
		Name: "pro", DisplayName: "Pro", PlatformFee: "49.00", MaxClusters: 5,
		ObservabilityPool: &client.PlanObservabilityPool{MaxActiveSeries: 50000, RetentionDays: 90},
	}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "get", "pro"); err != nil {
		t.Fatal(err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, want := range []string{"Name:", "Platform Fee:", "49.00", "Active Series:", "50000", "Retention (days):", "90"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
}

// TestPlanListWorksWithoutCredentials asserts plan browsing doesn't require
// a logged-in context. The endpoint is public and the CLI now routes through
// Factory.PublicClient, which never asks for a tenant or token.
func TestPlanListWorksWithoutCredentials(t *testing.T) {
	fake := clienttest.New()
	fake.Plans["starter"] = &client.Plan{Name: "starter"}

	// Factory with a config file that has NO contexts — as a first-time
	// user would see it.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{APIVersion: config.APIVersion, Kind: config.Kind}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.PublicClient = func() (client.Interface, error) { return fake, nil }

	// f.Client should panic or error — we MUST NOT route through it.
	f.Client = func() (client.Interface, error) {
		t.Fatal("plan list tried to authenticate — must use PublicClient")
		return nil, nil
	}

	if err := run(NewCmd(f), "list"); err != nil {
		t.Fatalf("plan list failed without credentials: %v", err)
	}
}

func TestPlanGetNotFoundMapsToExit4(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := run(NewCmd(f), "get", "enterprise")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}
