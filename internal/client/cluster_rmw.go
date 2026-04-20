package client

import (
	"context"
	"errors"
)

// ClusterMutator lets UpdateClusterRMW callers express the intended change
// as a function over the current resource state. Return nil to skip the
// PATCH entirely (no-op update — safe to surface as a success).
type ClusterMutator func(current *Cluster) *PatchClusterRequest

// ErrRMWContention is returned when UpdateClusterRMW hits two consecutive
// 412 ETag mismatches (i.e., another writer updated the resource each time
// the CLI tried). Surfaces as cli.ExitConflict.
var ErrRMWContention = errors.New("concurrent update contention; retry")

// UpdateClusterRMW is the safe read-modify-write helper for `cluster update`.
// Flow: GET → mutator → PATCH with If-Match. On 412 (another writer landed
// in the meantime), re-GET and retry once. This matches the pattern
// described in docs/api-client.md.
//
// Passing a nil PatchClusterRequest from the mutator is a no-op — the
// function returns the most recent GET result and no error.
func (c *Client) UpdateClusterRMW(ctx context.Context, name string, mutate ClusterMutator) (*Cluster, error) {
	for attempt := 0; attempt < 2; attempt++ {
		current, etag, err := c.GetCluster(ctx, name)
		if err != nil {
			return nil, err
		}

		patch := mutate(current)
		if patch == nil {
			return current, nil
		}

		updated, _, err := c.UpdateCluster(ctx, name, etag, *patch)
		if err == nil {
			return updated, nil
		}
		if !IsPreconditionFailed(err) {
			return nil, err
		}
		// 412 — fall through to retry.
	}
	return nil, ErrRMWContention
}
