package printer

import (
	"fmt"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// TenantDetailColumns renders the full tenant view used by
// `kupe tenant get`. Status fields (phase, cluster count, pool, current
// usage) are surfaced as individual rows — the raw JSON/YAML views still
// show everything via -o json.
func TenantDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return tenant(v).Name }},
		{Name: "Display Name", Get: func(v any) string {
			t := tenant(v)
			if t.DisplayName == "" {
				return t.Name
			}
			return t.DisplayName
		}},
		{Name: "Contact Email", Get: func(v any) string { return tenant(v).ContactEmail }},
		{Name: "Plan", Get: func(v any) string { return tenant(v).Plan }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(tenantPhase(tenant(v)), colorEnabled)
		}},
		{Name: "Cluster Count", Get: func(v any) string { return fmtInt(tenantStatus(v).ClusterCount) }},
		{Name: "Pool", Get: func(v any) string { return formatPool(tenantStatus(v).PoolResources) }},
		{Name: "Allocated", Get: func(v any) string { return formatPool(tenantStatus(v).AllocatedResources) }},
		{Name: "Period", Get: func(v any) string { return periodRange(tenantUsage(v)) }},
		{Name: "Estimated Total", Get: func(v any) string {
			u := tenantUsage(v)
			if u == nil || u.EstimatedTotal == "" {
				return ""
			}
			return u.EstimatedTotal + " " + u.Currency
		}},
		{Name: "Members", Get: func(v any) string {
			return fmt.Sprintf("%d", len(tenant(v).Members))
		}},
		{Name: "Created", Get: func(v any) string {
			t := tenant(v)
			age := ageFromRFC3339(t.CreatedAt)
			if age == "" {
				return t.CreatedAt
			}
			return t.CreatedAt + " (" + age + " ago)"
		}},
	}
}

func tenant(v any) *client.Tenant {
	switch x := v.(type) {
	case client.Tenant:
		return &x
	case *client.Tenant:
		return x
	}
	return &client.Tenant{}
}

func tenantStatus(v any) client.TenantStatus {
	t := tenant(v)
	if t == nil || t.Status == nil {
		return client.TenantStatus{}
	}
	return *t.Status
}

func tenantPhase(t *client.Tenant) string {
	if t == nil || t.Status == nil {
		return ""
	}
	return t.Status.Phase
}

func tenantUsage(v any) *client.TenantCurrentUsage {
	t := tenant(v)
	if t == nil || t.Status == nil {
		return nil
	}
	return t.Status.CurrentUsage
}

func formatPool(p *client.ResourcePool) string {
	if p == nil {
		return "-"
	}
	cpu, mem, stor := p.CPU, p.Memory, p.Storage
	if cpu == "" {
		cpu = "-"
	}
	if mem == "" {
		mem = "-"
	}
	if stor == "" {
		stor = "-"
	}
	return fmt.Sprintf("CPU=%s MEM=%s STORAGE=%s", cpu, mem, stor)
}

func periodRange(u *client.TenantCurrentUsage) string {
	if u == nil || (u.PeriodStart == "" && u.PeriodEnd == "") {
		return ""
	}
	return u.PeriodStart + " → " + u.PeriodEnd
}

func fmtInt(n int64) string { return fmt.Sprintf("%d", n) }
