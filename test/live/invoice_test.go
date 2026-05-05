//go:build live

package live

import "testing"

// TestInvoiceList drives the read-only invoice surface. A new testing
// tenant may genuinely have zero invoices (no closed billing periods
// yet), so we treat empty as a soft-pass and only assert shape on
// non-empty responses.
func TestInvoiceList(t *testing.T) {
	var listed []map[string]any
	runCLIJSON(t, &listed, "invoice", "list")
	if len(listed) == 0 {
		t.Skip("no invoices for testing tenant — skipping list-shape assertions")
	}
	for i, inv := range listed {
		if inv["name"] == "" {
			t.Errorf("invoice[%d] missing name", i)
		}
		if inv["periodStart"] == "" {
			t.Errorf("invoice[%d] missing periodStart", i)
		}
	}
}

// TestInvoiceGet pulls one invoice by name (the first from list, if any).
func TestInvoiceGet(t *testing.T) {
	var listed []map[string]any
	runCLIJSON(t, &listed, "invoice", "list")
	if len(listed) == 0 {
		t.Skip("no invoices to fetch")
	}
	name, _ := listed[0]["name"].(string)
	if name == "" {
		t.Skip("first invoice has no name")
	}

	var got map[string]any
	runCLIJSON(t, &got, "invoice", "get", name)
	if got["name"] != name {
		t.Errorf("invoice get name=%v; want %q", got["name"], name)
	}
}
