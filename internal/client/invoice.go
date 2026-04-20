package client

import (
	"context"
	"net/http"
)

// Invoice mirrors kupe-api's invoiceResponse. Name is typically the
// billing period in YYYY-MM form (e.g. "2026-03"). Status carries the
// totals + line items for consumption by the CLI's invoice printer and
// by downstream scripts via -o json.
type Invoice struct {
	Name            string         `json:"name" yaml:"name"`
	BillingPeriod   *BillingPeriod `json:"billingPeriod,omitempty" yaml:"billingPeriod,omitempty"`
	Status          InvoiceStatus  `json:"status" yaml:"status"`
	ResourceVersion string         `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string         `json:"createdAt" yaml:"createdAt"`
}

// BillingPeriod is the RFC3339 start/end range the invoice covers.
type BillingPeriod struct {
	Start string `json:"start" yaml:"start"`
	End   string `json:"end" yaml:"end"`
}

// InvoiceStatus holds the totals and the line-item breakdown. Phase
// values: Open, Finalised, Paid, Void.
type InvoiceStatus struct {
	Phase          string           `json:"phase,omitempty" yaml:"phase,omitempty"`
	LineItems      []map[string]any `json:"lineItems,omitempty" yaml:"lineItems,omitempty"`
	Subtotal       string           `json:"subtotal,omitempty" yaml:"subtotal,omitempty"`
	CreditsApplied string           `json:"creditsApplied,omitempty" yaml:"creditsApplied,omitempty"`
	Total          string           `json:"total,omitempty" yaml:"total,omitempty"`
	Currency       string           `json:"currency,omitempty" yaml:"currency,omitempty"`
}

// ListInvoices returns every invoice for the tenant, ordered by billing
// period. Read-only (readonly role sufficient).
func (c *Client) ListInvoices(ctx context.Context) ([]Invoice, error) {
	var resp struct {
		Items []Invoice `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("invoices"), nil, &resp)
	return resp.Items, err
}

// GetInvoice fetches a single invoice by name (billing period).
func (c *Client) GetInvoice(ctx context.Context, name string) (*Invoice, error) {
	var inv Invoice
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("invoices", name), nil, &inv)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}
