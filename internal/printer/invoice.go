package printer

import "github.com/kupecloud/kupe-cli/internal/client"

// InvoiceColumns is the table view for `kupe invoice list`.
func InvoiceColumns() Columns {
	return Columns{
		{Name: "PERIOD", Get: func(v any) string { return invoice(v).Name }},
		{Name: "PHASE", Get: func(v any) string { return invoice(v).Status.Phase }},
		{Name: "SUBTOTAL", Get: func(v any) string { return invoice(v).Status.Subtotal }},
		{Name: "CREDITS", Get: func(v any) string { return invoice(v).Status.CreditsApplied }},
		{Name: "TOTAL", Get: func(v any) string { return invoice(v).Status.Total }},
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
		{Name: "Period", Get: func(v any) string { return invoice(v).Name }},
		{Name: "Phase", Get: func(v any) string { return invoice(v).Status.Phase }},
		{Name: "Start", Get: func(v any) string {
			if p := invoice(v).BillingPeriod; p != nil {
				return p.Start
			}
			return ""
		}},
		{Name: "End", Get: func(v any) string {
			if p := invoice(v).BillingPeriod; p != nil {
				return p.End
			}
			return ""
		}},
		{Name: "Subtotal", Get: func(v any) string { return invoice(v).Status.Subtotal }},
		{Name: "Credits Applied", Get: func(v any) string { return invoice(v).Status.CreditsApplied }},
		{Name: "Total", Get: func(v any) string {
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
