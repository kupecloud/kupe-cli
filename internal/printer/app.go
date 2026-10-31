package printer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// App and AppEnvironment render desired configuration and independent status.
var App = Resource[client.App]{Columns: appColumns, DetailColumns: appDetailColumns, Name: func(a *client.App) string { return a.Name }}
var AppEnvironment = Resource[client.AppEnvironment]{Columns: appEnvironmentColumns, DetailColumns: appEnvironmentDetailColumns, Name: func(e *client.AppEnvironment) string { return e.Environment }}

func appColumns(color bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return rowOf[client.App](v).Name }},
		{Name: "PHASE", Get: func(v any) string { return ux.ColorPhase(valueOf(rowOf[client.App](v).Status).Phase, color) }},
		{Name: "ENVIRONMENTS", Get: func(v any) string { return fmt.Sprint(len(valueOf(rowOf[client.App](v).Status).Environments)) }},
	}
}
func appDetailColumns(color bool) Columns {
	return append(appColumns(color), Column{Name: "Deletion protection", Get: func(v any) string { return onOff(rowOf[client.App](v).DeletionProtection) }}, Column{Name: "Environment summary", Get: func(v any) string {
		var rows []string
		for _, e := range valueOf(rowOf[client.App](v).Status).Environments {
			rows = append(rows, e.Name+" "+e.Phase+" "+e.Address)
		}
		return strings.Join(rows, "\n")
	}})
}
func appEnvironmentColumns(color bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return rowOf[client.AppEnvironment](v).Environment }},
		{Name: "CLUSTER", Get: func(v any) string { return rowOf[client.AppEnvironment](v).Cluster }},
		{Name: "PHASE", Get: func(v any) string { return ux.ColorPhase(valueOf(rowOf[client.AppEnvironment](v).Status).Phase, color) }},
		{Name: "ADDRESS", Get: func(v any) string { return valueOf(rowOf[client.AppEnvironment](v).Status).Address }},
	}
}
func appEnvironmentDetailColumns(color bool) Columns {
	cols := appEnvironmentColumns(color)
	add := func(name string, get func(*client.AppEnvironment) string) {
		cols = append(cols, Column{Name: name, Get: func(v any) string { return get(rowOf[client.AppEnvironment](v)) }})
	}
	add("App", func(e *client.AppEnvironment) string { return e.App })
	add("Deletion protection", func(e *client.AppEnvironment) string { return onOff(e.DeletionProtection) })
	add("Desired web", func(e *client.AppEnvironment) string { return appJSON(e.Web) })
	add("Vault references", func(e *client.AppEnvironment) string { return appJSON(e.Secrets) })
	add("Desired domains", func(e *client.AppEnvironment) string { return strings.Join(e.Domains, ", ") })
	add("Namespace", func(e *client.AppEnvironment) string { return valueOf(e.Status).Namespace })
	add("Deployment", func(e *client.AppEnvironment) string {
		d := valueOf(valueOf(e.Status).Deployment)
		return strings.TrimSpace(appReleaseName(d.Release) + " " + d.Phase + " " + d.Reason)
	})
	add("Requested release", func(e *client.AppEnvironment) string { return appReleaseName(valueOf(e.Status).RequestedRelease) })
	add("Active release", func(e *client.AppEnvironment) string { return appReleaseName(valueOf(e.Status).ActiveRelease) })
	add("Last successful release", func(e *client.AppEnvironment) string { return appReleaseName(valueOf(e.Status).LastSuccessfulRelease) })
	add("Recovery", func(e *client.AppEnvironment) string {
		r := valueOf(valueOf(e.Status).Recovery)
		return strings.TrimSpace(r.Phase + " " + appReleaseName(r.Release))
	})
	add("Domain records", func(e *client.AppEnvironment) string {
		var rows []string
		for _, d := range valueOf(e.Status).Domains {
			rows = append(rows, strings.TrimSpace(d.Host+" "+d.Phase+" "+d.Reason+" "+d.Record))
		}
		return strings.Join(rows, "\n")
	})
	add("Secret status", func(e *client.AppEnvironment) string {
		var rows []string
		for _, s := range valueOf(e.Status).Secrets {
			row := s.Name + " " + s.Phase
			if len(s.MissingKeys) > 0 {
				row += " (missing: " + strings.Join(s.MissingKeys, ", ") + ")"
			}
			rows = append(rows, row)
		}
		return strings.Join(rows, "\n")
	})
	for i := range cols {
		get := cols[i].Get
		cols[i].Get = func(v any) string {
			value := get(v)
			if value == "" {
				return "-"
			}
			return value
		}
	}
	return cols
}
func appReleaseName(ref *client.AppReleaseReference) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}
func appJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
