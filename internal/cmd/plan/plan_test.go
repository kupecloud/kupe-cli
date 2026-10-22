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

// TestPlanGet_DatabaseLimitInStructuredOutput keeps an unlimited plan's 0
// in json and yaml output, and leaves the field out, rather than writing
// a 0, when the server did not report it.
func TestPlanGet_DatabaseLimitInStructuredOutput(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		maxDatabases *int64
		want         string
		wantAbsent   bool
	}{
		{name: "json unlimited", output: "json", maxDatabases: new(int64(0)), want: `"maxDatabases": 0`},
		{name: "json limited", output: "json", maxDatabases: new(int64(25)), want: `"maxDatabases": 25`},
		{name: "json not reported", output: "json", wantAbsent: true},
		{name: "yaml unlimited", output: "yaml", maxDatabases: new(int64(0)), want: "maxDatabases: 0"},
		{name: "yaml not reported", output: "yaml", wantAbsent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Plans["pro"] = &client.Plan{Name: "pro", MaxClusters: 5, MaxDatabases: tt.maxDatabases}
			f := factoryWith(t, fake)
			if err := run(NewCmd(f), "get", "pro", "-o", tt.output); err != nil {
				t.Fatal(err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String()
			if tt.wantAbsent {
				if strings.Contains(out, "maxDatabases") {
					t.Errorf("unreported limit written:\n%s", out)
				}
				return
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

// TestPlan_RegistryLimitInOutput shows the registry storage cap in the
// table and detail views, keeps it under registry.maxStorage in json and
// yaml, and leaves the registry block out when the server sent none.
func TestPlan_RegistryLimitInOutput(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		registry   *client.PlanRegistry
		want       []string
		wantAbsent string
	}{
		{name: "list table", args: []string{"list"}, registry: &client.PlanRegistry{MaxStorage: "5Gi"}, want: []string{"MAX-REGISTRY-STORAGE", "5Gi"}},
		{name: "list table no registry", args: []string{"list"}, want: []string{"MAX-REGISTRY-STORAGE"}, wantAbsent: "Gi"},
		{name: "get detail", args: []string{"get", "pro"}, registry: &client.PlanRegistry{MaxStorage: "5Gi"}, want: []string{"Max Registry Storage:", "5Gi"}},
		{name: "get json", args: []string{"get", "pro", "-o", "json"}, registry: &client.PlanRegistry{MaxStorage: "5Gi"}, want: []string{`"registry": {`, `"maxStorage": "5Gi"`}},
		{name: "get json unlimited", args: []string{"get", "pro", "-o", "json"}, registry: &client.PlanRegistry{MaxStorage: "0"}, want: []string{`"maxStorage": "0"`}},
		{name: "get json no registry", args: []string{"get", "pro", "-o", "json"}, wantAbsent: "registry"},
		{name: "list json", args: []string{"list", "-o", "json"}, registry: &client.PlanRegistry{MaxStorage: "5Gi"}, want: []string{`"maxStorage": "5Gi"`}},
		{name: "get yaml", args: []string{"get", "pro", "-o", "yaml"}, registry: &client.PlanRegistry{MaxStorage: "5Gi"}, want: []string{"registry:", "maxStorage: 5Gi"}},
		{name: "get yaml no registry", args: []string{"get", "pro", "-o", "yaml"}, wantAbsent: "registry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := clienttest.New()
			fake.Plans["pro"] = &client.Plan{Name: "pro", MaxClusters: 5, Registry: tt.registry}
			f := factoryWith(t, fake)
			if err := run(NewCmd(f), tt.args...); err != nil {
				t.Fatal(err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String()
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
			if tt.wantAbsent != "" && strings.Contains(out, tt.wantAbsent) {
				t.Errorf("output has %q:\n%s", tt.wantAbsent, out)
			}
		})
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
