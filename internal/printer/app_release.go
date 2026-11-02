package printer

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
)

var AppRelease = Resource[client.AppRelease]{Columns: appReleaseColumns, DetailColumns: appReleaseDetails, Name: func(r *client.AppRelease) string { return r.Name }}
var AppImage = Resource[client.ImageResolution]{Columns: appImageColumns, DetailColumns: appImageColumns, Name: func(r *client.ImageResolution) string { return r.Reference }}
var AppRetry = Resource[client.AppRetry]{Columns: appRetryColumns, DetailColumns: appRetryColumns, Name: func(r *client.AppRetry) string { return r.App + "/" + r.Environment }}

func appReleaseColumns(_ bool) Columns {
	return Columns{
		{Name: "NUMBER", Get: func(v any) string { return fmt.Sprint(rowOf[client.AppRelease](v).Number) }},
		{Name: "TRIGGER", Get: func(v any) string { return rowOf[client.AppRelease](v).Trigger }},
		{Name: "PHASE", Get: func(v any) string { return rowOf[client.AppRelease](v).Status.Phase }},
		{Name: "RECOVERY", Get: func(v any) string {
			r := rowOf[client.AppRelease](v)
			if r.Status.Phase != "Failed" || r.Status.Recovery == nil {
				return "-"
			}
			return strings.TrimSpace(r.Status.Recovery.Phase + " " + appReleaseName(r.Status.Recovery.Release))
		}},
		{Name: "IMAGE", Get: func(v any) string {
			r := rowOf[client.AppRelease](v)
			if r.Status.ImageDigest != "" {
				return r.Status.ImageDigest
			}
			return r.Runtime.Image
		}},
		{Name: "STARTED", Get: func(v any) string { return rowOf[client.AppRelease](v).Status.StartedAt }},
		{Name: "COMPLETED", Get: func(v any) string { return rowOf[client.AppRelease](v).Status.CompletedAt }},
		{Name: "REASON", Get: func(v any) string { return rowOf[client.AppRelease](v).Status.Reason }},
	}
}
func appReleaseDetails(color bool) Columns {
	return append(appReleaseColumns(color),
		Column{Name: "Name", Get: func(v any) string { return rowOf[client.AppRelease](v).Name }},
		Column{Name: "Deployment revision", Get: func(v any) string { return fmt.Sprint(rowOf[client.AppRelease](v).DeploymentRevision) }},
		Column{Name: "Chart revision", Get: func(v any) string { return rowOf[client.AppRelease](v).Chart.Revision }},
		Column{Name: "Message", Get: func(v any) string { return rowOf[client.AppRelease](v).Status.Message }},
	)
}
func appImageColumns(_ bool) Columns {
	return Columns{
		{Name: "Reference", Get: func(v any) string { return rowOf[client.ImageResolution](v).Reference }},
		{Name: "Platforms", Get: func(v any) string {
			var result []string
			for _, p := range rowOf[client.ImageResolution](v).Platforms {
				name := p.OS + "/" + p.Architecture
				if p.Variant != "" {
					name += "/" + p.Variant
				}
				result = append(result, name)
			}
			return strings.Join(result, ", ")
		}},
		{Name: "User", Get: func(v any) string { return rowOf[client.ImageResolution](v).User }},
		{Name: "Exposed TCP ports", Get: func(v any) string { return fmt.Sprint(rowOf[client.ImageResolution](v).ExposedPorts) }},
	}
}
func appRetryColumns(_ bool) Columns {
	return Columns{
		{Name: "App", Get: func(v any) string { return rowOf[client.AppRetry](v).App }},
		{Name: "Environment", Get: func(v any) string { return rowOf[client.AppRetry](v).Environment }},
		{Name: "Deployment revision", Get: func(v any) string { return fmt.Sprint(rowOf[client.AppRetry](v).DeploymentRevision) }},
		{Name: "Generation", Get: func(v any) string { return fmt.Sprint(rowOf[client.AppRetry](v).Generation) }},
	}
}
