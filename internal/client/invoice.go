package client

import (
	"context"
	"net/http"
)

// Invoice mirrors kupe-api's invoiceResponse. Names are server-controlled:
// usually "{tenant}-{YYYYMMDD}" for the period start (e.g. "acme-20260301"),
// with a "-final" suffix for final invoices issued on cancellation/deletion
// and a "{tenant}-{YYYYMMDD-HHMMSS}" form when two periods start on the same
// date. Not a guessable format; users should always look up the actual name
// via ListInvoices.
// Status carries the totals + line items for consumption by the CLI's
// invoice printer and by downstream scripts via -o json.
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
// values mirror the operator: Draft, Billed, Paid, PastDue.
//
// Operator-internal status fields (chargeState, chargeSubmittedAt,
// creditsDeducted, lastWebhookEventID, isFinal) are intentionally NOT
// exposed by kupe-api so they don't appear here either.
type InvoiceStatus struct {
	Phase          string           `json:"phase,omitempty" yaml:"phase,omitempty"`
	IssuedAt       string           `json:"issuedAt,omitempty" yaml:"issuedAt,omitempty"`
	BilledUntil    string           `json:"billedUntil,omitempty" yaml:"billedUntil,omitempty"`
	LineItems      []map[string]any `json:"lineItems,omitempty" yaml:"lineItems,omitempty"`
	Subtotal       string           `json:"subtotal,omitempty" yaml:"subtotal,omitempty"`
	CreditsApplied string           `json:"creditsApplied,omitempty" yaml:"creditsApplied,omitempty"`
	Tax            string           `json:"tax,omitempty" yaml:"tax,omitempty"`
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

// GetInvoice fetches a single invoice by name. Names are server-controlled
// (usually "{tenant}-{YYYYMMDD}"; variants exist) — list invoices rather
// than constructing names.
func (c *Client) GetInvoice(ctx context.Context, name string) (*Invoice, error) {
	var inv Invoice
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("invoices", name), nil, &inv)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}
