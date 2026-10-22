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
		{Name: "MAX-DATABASES", Get: func(v any) string { return fmtLimit(plan(v).MaxDatabases) }},
		{Name: "MAX-REGISTRY-STORAGE", Get: func(v any) string { return fmtRegistryLimit(plan(v).Registry) }},
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
		{Name: "Max Databases", Get: func(v any) string { return fmtLimit(plan(v).MaxDatabases) }},
		{Name: "Max Registry Storage", Get: func(v any) string { return fmtRegistryLimit(plan(v).Registry) }},
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

func plan(v any) *client.Plan { return rowOf[client.Plan](v) }

// fmtLimit renders a plan limit the server may not report as "-" when
// absent, because 0 means unlimited and would overstate it.
func fmtLimit(n *int64) string {
	if n == nil {
		return "-"
	}
	return fmtInt(*n)
}

// fmtRegistryLimit renders the registry storage cap as the server sends
// it ("0" is unlimited, as for the count limits), and "-" when the server
// reports no registry allowance.
func fmtRegistryLimit(r *client.PlanRegistry) string {
	if r == nil || r.MaxStorage == "" {
		return "-"
	}
	return r.MaxStorage
}
