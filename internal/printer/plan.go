package printer

import "github.com/kupecloud/kupe-cli/internal/client"

// PlanColumns is the table view for `kupe plan list`.
func PlanColumns() Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return plan(v).Name }},
		{Name: "DISPLAY", Get: func(v any) string {
			p := plan(v)
			if p.DisplayName == "" {
				return p.Name
			}
			return p.DisplayName
		}},
		{Name: "FEE", Get: func(v any) string { return plan(v).PlatformFee }},
		{Name: "MAX-CLUSTERS", Get: func(v any) string { return fmtInt(plan(v).MaxClusters) }},
		{Name: "POOL", Get: func(v any) string { return formatPool(plan(v).ResourcePool) }},
		{Name: "METRICS-SERIES", Wide: true, Get: func(v any) string {
			if o := plan(v).ObservabilityPool; o != nil {
				return fmtInt(o.MaxActiveSeries)
			}
			return ""
		}},
		{Name: "LOG-GB", Wide: true, Get: func(v any) string {
			if o := plan(v).ObservabilityPool; o != nil {
				return fmtInt(o.LogIngestGB)
			}
			return ""
		}},
	}
}

// PlanDetailColumns is the key:value view for `kupe plan get`.
func PlanDetailColumns() Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return plan(v).Name }},
		{Name: "Display Name", Get: func(v any) string {
			p := plan(v)
			if p.DisplayName == "" {
				return p.Name
			}
			return p.DisplayName
		}},
		{Name: "Platform Fee", Get: func(v any) string { return plan(v).PlatformFee }},
		{Name: "Max Clusters", Get: func(v any) string { return fmtInt(plan(v).MaxClusters) }},
		{Name: "Resource Pool", Get: func(v any) string { return formatPool(plan(v).ResourcePool) }},
		{Name: "Active Series", Get: func(v any) string {
			if o := plan(v).ObservabilityPool; o != nil {
				return fmtInt(o.MaxActiveSeries)
			}
			return ""
		}},
		{Name: "Log Ingest (GB)", Get: func(v any) string {
			if o := plan(v).ObservabilityPool; o != nil {
				return fmtInt(o.LogIngestGB)
			}
			return ""
		}},
		{Name: "Retention (days)", Get: func(v any) string {
			if o := plan(v).ObservabilityPool; o != nil {
				return fmtInt(int64(o.RetentionDays))
			}
			return ""
		}},
		{Name: "Max Receivers", Get: func(v any) string {
			if o := plan(v).ObservabilityPool; o != nil {
				return fmtInt(int64(o.MaxReceivers))
			}
			return ""
		}},
	}
}

func plan(v any) *client.Plan {
	switch x := v.(type) {
	case client.Plan:
		return &x
	case *client.Plan:
		return x
	}
	return &client.Plan{}
}
