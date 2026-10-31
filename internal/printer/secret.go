package printer

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// SecretColumns are the default list columns for `kupe secret list`.
// DELIVERED counts current targets confirmed Synced; PATH is wide-only because paths
// can be long.
func SecretColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return secret(v).Name }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(secretPhase(secret(v)), colorEnabled)
		}},
		{Name: "DELIVERED", Get: func(v any) string { return secretDeliveryCount(secret(v)) }},
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
		{Name: "Sync targets", Get: func(v any) string { return renderSync(secret(v)) }},
		{Name: "Age", Get: func(v any) string { return ageFromRFC3339(secret(v).CreatedAt) }},
	}
}

func secret(v any) *client.Secret { return rowOf[client.Secret](v) }

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

func secretDeliveryCount(s *client.Secret) string {
	delivered := 0
	if s.Status != nil && s.Status.ObservedGeneration == s.Generation {
		for _, target := range s.Sync {
			for _, state := range s.Status.SyncStatuses {
				if state.Cluster == target.Cluster && state.Namespace == target.Namespace && state.Phase == "Synced" {
					delivered++
					break
				}
			}
		}
	}
	return fmt.Sprintf("%d/%d", delivered, len(s.Sync))
}

func renderSync(s *client.Secret) string {
	var lines []string
	seen := map[string]bool{}
	for _, target := range s.Sync {
		name := target.SecretName
		if name == "" {
			name = s.Name
		}
		keys := "all"
		if len(target.Keys) > 0 {
			keys = strings.Join(target.Keys, ",")
		}
		phase, reason := "Pending", ""
		if s.Status != nil {
			for _, state := range s.Status.SyncStatuses {
				if state.Cluster == target.Cluster && state.Namespace == target.Namespace {
					phase, reason = state.Phase, state.Reason
					if state.SecretName != "" {
						name = state.SecretName
					}
					break
				}
			}
		}
		seen[target.Cluster+"/"+target.Namespace] = true
		lines = append(lines, syncLine(target.Cluster, target.Namespace, name, keys, phase, reason))
	}
	if s.Status != nil {
		for _, state := range s.Status.SyncStatuses {
			if !seen[state.Cluster+"/"+state.Namespace] {
				lines = append(lines, syncLine(state.Cluster, state.Namespace, state.SecretName, "-", state.Phase, state.Reason))
			}
		}
	}
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, ", ")
}

func syncLine(cluster, namespace, name, keys, phase, reason string) string {
	line := fmt.Sprintf("%s/%s as %s (keys: %s; %s", cluster, namespace, name, keys, phase)
	if reason != "" {
		line += "; " + reason
	}
	return line + ")"
}
