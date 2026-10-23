package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
				PeriodStart: "2026-04-01",
				PeriodEnd:   "2026-05-01",
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
	for _, want := range []string{"acme", "Acme Corp", "pro", "Active", "CPU=8 MEM=32 STORAGE=200", "2026-04-01 → 2026-05-01", "Members:"} {
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
				Compute: &client.TenantCurrentUsageCompute{
					CPUCoreHours: "2976.00",
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
	if compute["cpuCoreHours"] != "2976.00" {
		t.Fatalf("nested compute.cpuCoreHours lost in JSON: %+v", compute)
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

// TestTenantGet_NoAmountInAnyFormat serves a tenant from a kupe-api that
// sends the usage estimate's amounts (as one before CR-3a did) and the
// remaining credit balance, and checks no output format prints one: no
// prices in alpha (D-40, kupecloud/kupe-api#42). It goes through the real
// HTTP client, so a Tenant field that decodes an amount again fails here.
func TestTenantGet_NoAmountInAnyFormat(t *testing.T) {
	const tenantJSON = `{"name":"acme","displayName":"Acme Corp","plan":"pro","status":{"phase":"Active",` +
		`"currentUsage":{"periodStart":"2026-04-01","periodEnd":"2026-05-01","currency":"GBP",` +
		`"compute":{"cpuCoreHours":"2976.00","cpuCost":"89.28","memoryCost":"10.72","storageCost":"3.33"},` +
		`"observability":{"currentActiveSeries":1200,"metricsOverageCost":"4.44","logOverageCost":"5.55"},` +
		`"estimatedSubtotal":"156.15","estimatedCreditsApplied":"6.66","estimatedTotal":"149.49"},` +
		`"billing":{"remainingCredits":"77.77","nextBillingDate":"2026-05-01"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/tenants/acme" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, tenantJSON)
	}))
	t.Cleanup(srv.Close)

	// Any of these in the output is an amount or its label.
	banned := []string{"89.28", "10.72", "3.33", "4.44", "5.55", "156.15", "6.66", "149.49", "77.77", "GBP", "£",
		"Cost", "cost", "currency", "Currency", "estimated", "Estimated", "credit", "Credit", "Total", "total", "price"}
	for _, args := range [][]string{{"get"}, {"get", "-o", "json"}, {"get", "-o", "yaml"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := factoryWith(t, clienttest.New())
			f.Client = func() (client.Interface, error) {
				return client.New(srv.URL, "acme", "kupe_test", "kupe-cli-test/0.0.0"), nil
			}
			if err := run(NewCmd(f), args...); err != nil {
				t.Fatal(err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String()
			if !strings.Contains(out, "acme") {
				t.Fatalf("tenant not printed:\n%s", out)
			}
			for _, b := range banned {
				if strings.Contains(out, b) {
					t.Errorf("output carries %q:\n%s", b, out)
				}
			}
		})
	}
}
