package printer

import "github.com/kupecloud/kupe-cli/internal/client"

// InvoiceColumns is the table view for `kupe invoice list`.
func InvoiceColumns() Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return invoice(v).Name }},
		{Name: "PHASE", Get: func(v any) string { return invoice(v).Status.Phase }},
		{Name: "ISSUED", Get: func(v any) string { return invoice(v).Status.IssuedAt }},
		{Name: "SUBTOTAL", Get: func(v any) string { return invoice(v).Status.Subtotal }},
		{Name: "CREDITS", Get: func(v any) string { return invoice(v).Status.CreditsApplied }},
		{Name: "TAX", Wide: true, Get: func(v any) string { return invoice(v).Status.Tax }},
		// Totals are pre-tax by design — Paddle (merchant of record) adds
		// VAT/sales tax at payment, so the header says so explicitly.
		{Name: "TOTAL (EXCL. VAT)", Get: func(v any) string { return invoice(v).Status.Total }},
		{Name: "CURRENCY", Get: func(v any) string { return invoice(v).Status.Currency }},
		{Name: "START", Wide: true, Get: func(v any) string {
			if p := invoice(v).BillingPeriod; p != nil {
				return p.Start
			}
			return ""
		}},
		{Name: "END", Wide: true, Get: func(v any) string {
			if p := invoice(v).BillingPeriod; p != nil {
				return p.End
			}
			return ""
		}},
	}
}

// InvoiceDetailColumns is the key:value view for `kupe invoice get`. Line
// items are omitted here (they're long and structured) — users who want
// them use -o json / -o yaml.
func InvoiceDetailColumns() Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return invoice(v).Name }},
		{Name: "Phase", Get: func(v any) string { return invoice(v).Status.Phase }},
		{Name: "Issued", Get: func(v any) string { return invoice(v).Status.IssuedAt }},
		{Name: "Period Start", Get: func(v any) string {
			if p := invoice(v).BillingPeriod; p != nil {
				return p.Start
			}
			return ""
		}},
		{Name: "Period End", Get: func(v any) string {
			if p := invoice(v).BillingPeriod; p != nil {
				return p.End
			}
			return ""
		}},
		{Name: "Billed Until", Get: func(v any) string { return invoice(v).Status.BilledUntil }},
		{Name: "Subtotal", Get: func(v any) string { return invoice(v).Status.Subtotal }},
		{Name: "Credits Applied", Get: func(v any) string { return invoice(v).Status.CreditsApplied }},
		{Name: "Tax", Get: func(v any) string { return invoice(v).Status.Tax }},
		{Name: "Total (excl. VAT)", Get: func(v any) string {
			inv := invoice(v)
			if inv.Status.Total == "" {
				return ""
			}
			return inv.Status.Total + " " + inv.Status.Currency
		}},
		{Name: "Line Items", Get: func(v any) string {
			n := len(invoice(v).Status.LineItems)
			if n == 0 {
				return "(none)"
			}
			return fmtInt(int64(n)) + " (use -o json for details)"
		}},
	}
}

func invoice(v any) *client.Invoice {
	switch x := v.(type) {
	case client.Invoice:
		return &x
	case *client.Invoice:
		return x
	}
	return &client.Invoice{}
}
