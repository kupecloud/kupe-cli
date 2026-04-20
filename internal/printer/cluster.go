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
		{Name: "VERSION", Get: func(v any) string { return cluster(v).Version }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(phaseOf(cluster(v)), colorEnabled)
		}},
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
		{Name: "Version", Get: func(v any) string {
			c := cluster(v)
			running := statusOf(c).KubernetesVersion
			if running != "" && running != c.Version {
				return c.Version + " (running " + running + ")"
			}
			return c.Version
		}},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(phaseOf(cluster(v)), colorEnabled)
		}},
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
