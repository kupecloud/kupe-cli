package printer

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// SecretColumns are the default list columns for `kupe secret list`.
// SYNCS counts the sync-target entries; PATH is wide-only because paths
// can be long.
func SecretColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return secret(v).Name }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(secretPhase(secret(v)), colorEnabled)
		}},
		{Name: "SYNCS", Get: func(v any) string {
			return fmt.Sprintf("%d", len(secret(v).Sync))
		}},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(secret(v).CreatedAt) }},
		{Name: "PATH", Wide: true, Get: func(v any) string { return secret(v).SecretPath }},
		{Name: "CLUSTERS", Wide: true, Get: func(v any) string { return syncClusters(secret(v)) }},
	}
}

// SecretDetailColumns is the key:value layout used by `kupe secret get`.
func SecretDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return secret(v).Name }},
		{Name: "Path", Get: func(v any) string { return secret(v).SecretPath }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(secretPhase(secret(v)), colorEnabled)
		}},
		{Name: "Sync targets", Get: func(v any) string { return renderSync(secret(v).Sync) }},
		{Name: "Created", Get: func(v any) string {
			s := secret(v).CreatedAt
			if age := ageFromRFC3339(s); age != "" {
				return s + " (" + age + " ago)"
			}
			return s
		}},
	}
}

func secret(v any) *client.Secret {
	switch x := v.(type) {
	case client.Secret:
		return &x
	case *client.Secret:
		return x
	}
	return &client.Secret{}
}

func secretPhase(s *client.Secret) string {
	if s == nil || s.Status == nil {
		return ""
	}
	return s.Status.Phase
}

func syncClusters(s *client.Secret) string {
	if s == nil {
		return ""
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(s.Sync))
	for _, t := range s.Sync {
		if _, ok := seen[t.Cluster]; ok {
			continue
		}
		seen[t.Cluster] = struct{}{}
		names = append(names, t.Cluster)
	}
	return strings.Join(names, ",")
}

func renderSync(targets []client.SyncTarget) string {
	if len(targets) == 0 {
		return "(none)"
	}
	var lines []string
	for _, t := range targets {
		label := t.Cluster + "/" + t.Namespace
		if t.SecretName != "" {
			label += " as " + t.SecretName
		}
		lines = append(lines, label)
	}
	return strings.Join(lines, ", ")
}
