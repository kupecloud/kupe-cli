package invoice

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

func TestInvoiceList(t *testing.T) {
	fake := clienttest.New()
	fake.Invoices["acme-20260301"] = &client.Invoice{
		Name:   "acme-20260301",
		Status: client.InvoiceStatus{Phase: "Paid", IssuedAt: "2026-04-01T00:00:00Z"},
	}
	fake.Invoices["acme-20260201"] = &client.Invoice{
		Name:   "acme-20260201",
		Status: client.InvoiceStatus{Phase: "Paid"},
	}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "list"); err != nil {
		t.Fatal(err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, want := range []string{"NAME", "PHASE", "acme-20260301", "2026-04-01T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
}

func TestInvoiceGetJSONSurfacesLineItems(t *testing.T) {
	fake := clienttest.New()
	fake.Invoices["acme-20260301"] = &client.Invoice{
		Name: "acme-20260301",
		Status: client.InvoiceStatus{
			Phase: "Paid",
			LineItems: []client.InvoiceLineItem{
				{Description: "CPU", ResourceType: "cpu", Quantity: "2976.00"},
				{Description: "Memory", ResourceType: "memory", Quantity: "11904.00"},
			},
		},
	}
	f := factoryWith(t, fake)
	if err := run(NewCmd(f), "get", "acme-20260301", "-o", "json"); err != nil {
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
	err := run(NewCmd(f), "get", "acme-20250101")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}

// TestInvoice_NoAmountInAnyFormat serves invoices from a kupe-api that
// still sends totals, tax, currency and line-item prices, and checks no
// output format prints one: no prices in alpha (D-40,
// kupecloud/kupe-api#42). It goes through the real HTTP client, so an
// Invoice field that decodes an amount again fails here.
func TestInvoice_NoAmountInAnyFormat(t *testing.T) {
	const invoiceJSON = `{"name":"acme-20260301","billingPeriod":{"start":"2026-03-01T00:00:00Z","end":"2026-04-01T00:00:00Z"},` +
		`"paddleTransactionID":"txn_1","status":{"phase":"Paid","issuedAt":"2026-04-01T00:00:00Z","billedUntil":"2026-04-01T00:00:00Z",` +
		`"lineItems":[{"description":"CPU","cluster":"prod","resourceType":"cpu","quantity":"2976.00","unitPrice":"0.0300","amount":"89.28"}],` +
		`"subtotal":"120.00","creditsApplied":"20.00","tax":"7.77","total":"100.00","currency":"GBP"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/tenants/acme/invoices":
			fmt.Fprint(w, `{"items":[`+invoiceJSON+`]}`)
		case "/api/v1/tenants/acme/invoices/acme-20260301":
			fmt.Fprint(w, invoiceJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	// Any of these in the output is an amount or its label.
	banned := []string{"120.00", "20.00", "7.77", "100.00", "89.28", "0.0300", "GBP", "£",
		"subtotal", "Subtotal", "SUBTOTAL", "credit", "Credit", "CREDIT", "tax", "Tax", "TAX",
		"total", "Total", "TOTAL", "currency", "CURRENCY", "unitPrice", "amount", "price", "cost", "VAT"}
	for _, args := range [][]string{
		{"list"}, {"list", "-o", "wide"}, {"list", "-o", "json"}, {"list", "-o", "yaml"},
		{"get", "acme-20260301"}, {"get", "acme-20260301", "-o", "json"}, {"get", "acme-20260301", "-o", "yaml"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := factoryWith(t, clienttest.New())
			f.Client = func() (client.Interface, error) {
				return client.New(srv.URL, "acme", "kupe_test", "kupe-cli-test/0.0.0"), nil
			}
			if err := run(NewCmd(f), args...); err != nil {
				t.Fatal(err)
			}
			out := f.IOStreams.Out.(interface{ String() string }).String()
			if !strings.Contains(out, "acme-20260301") {
				t.Fatalf("invoice not printed:\n%s", out)
			}
			for _, b := range banned {
				if strings.Contains(out, b) {
					t.Errorf("output carries %q:\n%s", b, out)
				}
			}
		})
	}
}
