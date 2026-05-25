package printer

import (
	"time"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// ClusterColumns returns the column specs for rendering clusters. colorEnabled
// controls whether the PHASE cell is ANSI-coloured; it comes from the
// IOStreams the command's factory handed down.
func ClusterColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return cluster(v).Name }},
		{Name: "TYPE", Get: func(v any) string { return cluster(v).Type }},
		{Name: "VERSION", Get: func(v any) string { return clusterVersionDisplay(cluster(v)) }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(phaseOf(cluster(v)), colorEnabled)
		}},
		{Name: "HA", Get: func(v any) string { return haDisplay(cluster(v)) }},
		{Name: "CPU", Get: func(v any) string { return resourceOf(cluster(v)).CPU }},
		{Name: "MEM", Get: func(v any) string { return resourceOf(cluster(v)).Memory }},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(cluster(v).CreatedAt) }},
		{Name: "ENDPOINT", Wide: true, Get: func(v any) string { return statusOf(cluster(v)).Endpoint }},
		{Name: "K8S-VERSION", Wide: true, Get: func(v any) string { return statusOf(cluster(v)).KubernetesVersion }},
		{Name: "STORAGE", Wide: true, Get: func(v any) string { return resourceOf(cluster(v)).Storage }},
	}
}

// ClusterDetailColumns is the column list used by PrintDetails for
// `kupe cluster get`. Returns fields in a key:value layout rather than a
// table. Wide entries from ClusterColumns are always shown in details view.
func ClusterDetailColumns(colorEnabled bool) Columns {
	cols := Columns{
		{Name: "Name", Get: func(v any) string { return cluster(v).Name }},
		{Name: "Display Name", Get: func(v any) string {
			if d := cluster(v).DisplayName; d != "" {
				return d
			}
			return cluster(v).Name
		}},
		{Name: "Type", Get: func(v any) string { return cluster(v).Type }},
		{Name: "Version", Get: func(v any) string { return clusterVersionDisplay(cluster(v)) }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(phaseOf(cluster(v)), colorEnabled)
		}},
		{Name: "High Availability", Get: func(v any) string { return haDetailDisplay(cluster(v)) }},
		{Name: "Endpoint", Get: func(v any) string { return statusOf(cluster(v)).Endpoint }},
		{Name: "CPU", Get: func(v any) string { return resourceOf(cluster(v)).CPU }},
		{Name: "Memory", Get: func(v any) string { return resourceOf(cluster(v)).Memory }},
		{Name: "Storage", Get: func(v any) string { return resourceOf(cluster(v)).Storage }},
		{Name: "Created", Get: func(v any) string {
			c := cluster(v)
			age := ageFromRFC3339(c.CreatedAt)
			if age == "" {
				return c.CreatedAt
			}
			return c.CreatedAt + " (" + age + " ago)"
		}},
	}
	return cols
}

// haDisplay renders the compact HA cell for `cluster list`. Combines spec
// (the request) and status (operational reality) into one short string so
// the table stays readable:
//   - HA not requested → "off"
//   - HA requested but not yet configured → "pending"
//   - HA configured and cluster Running → "on"
//   - HA configured but cluster not Running → "degraded"
func haDisplay(c *client.Cluster) string {
	if c == nil || !c.HighAvailability {
		return "off"
	}
	st := statusOf(c)
	if !st.HAConfigured {
		return "pending"
	}
	if st.Phase != client.PhaseRunning {
		return "degraded"
	}
	return "on"
}

// haDetailDisplay is the expanded HA line for `cluster get`. Includes the
// billing-anchor timestamp so tenants can self-serve "when did HA charging
// start for this cluster?" without grepping invoice lines.
func haDetailDisplay(c *client.Cluster) string {
	if c == nil || !c.HighAvailability {
		return "off"
	}
	st := statusOf(c)
	switch {
	case !st.HAConfigured && st.Phase == client.PhaseMigrating:
		return "migrating (kine→etcd in progress, ~10 min downtime)"
	case !st.HAConfigured:
		return "pending (waiting for 3/3 replicas to be ready)"
	case st.Phase != client.PhaseRunning:
		return "degraded — enabled at " + st.HAEnabledAt
	default:
		return "on — enabled at " + st.HAEnabledAt
	}
}

// cluster asserts a table row's value into a client.Cluster. Accepts either
// a value or a pointer, so both []Cluster and []*Cluster work.
func cluster(v any) *client.Cluster {
	switch x := v.(type) {
	case client.Cluster:
		return &x
	case *client.Cluster:
		return x
	}
	return &client.Cluster{}
}

func phaseOf(c *client.Cluster) string {
	if c == nil || c.Status == nil {
		return ""
	}
	return c.Status.Phase
}

func statusOf(c *client.Cluster) client.ClusterStatus {
	if c == nil || c.Status == nil {
		return client.ClusterStatus{}
	}
	return *c.Status
}

// clusterVersionDisplay returns the most useful single string for the
// Version column / detail field:
//
//   - both desired (spec) and running (status) set & equal: just the value
//   - both set but differ (mid-upgrade or drift): "desired (running X)"
//   - only running known (server-defaulted at create time, no spec.version):
//     just the running value
//   - only desired known (cluster still provisioning): just the desired
//   - neither known: empty string
//
// The list and detail views share this helper so the rendering rule
// stays single-sourced.
func clusterVersionDisplay(c *client.Cluster) string {
	if c == nil {
		return ""
	}
	running := statusOf(c).KubernetesVersion
	switch {
	case c.Version == "" && running == "":
		return ""
	case c.Version == "":
		return running
	case running == "" || running == c.Version:
		return c.Version
	default:
		return c.Version + " (running " + running + ")"
	}
}

func resourceOf(c *client.Cluster) client.ClusterResource {
	if c == nil || c.Resources == nil {
		return client.ClusterResource{}
	}
	return *c.Resources
}

// ageFromRFC3339 renders a create-timestamp as a compact "12d", "3h", "45m",
// "30s" string. Matches kubectl's convention roughly — we don't go below
// seconds and don't bother with plural forms.
func ageFromRFC3339(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return shortDuration(time.Since(t))
}

func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	switch {
	case sec < 60:
		return itoaSuffix(sec, "s")
	case sec < 3600:
		return itoaSuffix(sec/60, "m")
	case sec < 86400:
		return itoaSuffix(sec/3600, "h")
	default:
		return itoaSuffix(sec/86400, "d")
	}
}

func itoaSuffix(n int, suffix string) string {
	// Avoid strconv for this hot path? No, it's not hot. Use fmt.Sprintf? No,
	// keep it simple and zero-alloc via byte builder.
	if n == 0 {
		return "0" + suffix
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf) + suffix
}
