package invoice

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

func TestInvoiceList(t *testing.T) {
	fake := clienttest.New()
	fake.Invoices["2026-03"] = &client.Invoice{
		Name:   "2026-03",
		Status: client.InvoiceStatus{Phase: "Paid", Subtotal: "120.00", Total: "100.00", Currency: "GBP"},
	}
	fake.Invoices["2026-02"] = &client.Invoice{
		Name:   "2026-02",
		Status: client.InvoiceStatus{Phase: "Paid", Subtotal: "90.00", Total: "90.00", Currency: "GBP"},
	}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "list"); err != nil {
		t.Fatal(err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, want := range []string{"PERIOD", "PHASE", "2026-03", "100.00", "GBP"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
}

func TestInvoiceGetJSONSurfacesLineItems(t *testing.T) {
	fake := clienttest.New()
	fake.Invoices["2026-03"] = &client.Invoice{
		Name: "2026-03",
		Status: client.InvoiceStatus{
			Phase: "Paid", Total: "100.00", Currency: "GBP",
			LineItems: []map[string]any{
				{"kind": "compute.cpu", "cost": "89.28"},
				{"kind": "compute.memory", "cost": "10.72"},
			},
		},
	}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "get", "2026-03", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	var got client.Invoice
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Status.LineItems) != 2 {
		t.Fatalf("line items not preserved: %+v", got.Status.LineItems)
	}
}

func TestInvoiceGetNotFoundMapsToExit4(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := run(NewCmd(f), "get", "2025-01")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}
