package secret

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kupecloud/kupe-cli/internal/client"
)

const (
	maxKeysPerTarget = 64
	maxSelectedKeys  = 1024
)

// selectKeys marks explicit target selections, so an RMW retry keeps the keys
// from its newly read object for every target the command did not override.
func selectKeys(targets []client.SyncTarget, rawKeys, allKeys []string) (map[string]bool, error) {
	explicit := map[string]bool{}
	indices := map[string]int{}
	for i, target := range targets {
		indices[target.Cluster+":"+target.Namespace] = i
	}
	total := 0
	for _, raw := range rawKeys {
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid --sync-key %q: expected cluster:namespace:key", raw)
		}
		id, key := parts[0]+":"+parts[1], parts[2]
		i, ok := indices[id]
		if !ok {
			return nil, fmt.Errorf("--sync-key target %q must be given with --sync", id)
		}
		if len(validation.IsConfigMapKey(key)) != 0 {
			return nil, fmt.Errorf("invalid --sync-key for target %q: keys use 1–253 letters, digits, '-', '_' or '.', and cannot be '.', '..' or start with '..'", id)
		}
		if slices.Contains(targets[i].Keys, key) {
			return nil, fmt.Errorf("repeated --sync-key %q for target %q", key, id)
		}
		targets[i].Keys = append(targets[i].Keys, key)
		explicit[id] = true
		total++
		if len(targets[i].Keys) > maxKeysPerTarget {
			return nil, fmt.Errorf("target %q accepts at most 64 keys", id)
		}
		if total > maxSelectedKeys {
			return nil, fmt.Errorf("a secret accepts at most 1,024 selected keys")
		}
	}
	for _, id := range allKeys {
		parts := strings.Split(id, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --all-keys %q: expected cluster:namespace", id)
		}
		if _, ok := indices[id]; !ok {
			return nil, fmt.Errorf("--all-keys target %q must be given with --sync", id)
		}
		if explicit[id] {
			return nil, fmt.Errorf("--sync-key and --all-keys cannot name the same target %q", id)
		}
		explicit[id] = true
	}
	return explicit, nil
}

func keepTargetKeys(targets []client.SyncTarget, current *client.Secret, explicit map[string]bool) []client.SyncTarget {
	result := slices.Clone(targets)
	for i, target := range result {
		if explicit[target.Cluster+":"+target.Namespace] {
			continue
		}
		for _, previous := range current.Sync {
			if previous.Cluster == target.Cluster && previous.Namespace == target.Namespace {
				result[i].Keys = slices.Clone(previous.Keys)
				break
			}
		}
	}
	return result
}
