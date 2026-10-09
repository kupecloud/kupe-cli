package client

import (
	"context"
	"errors"
)

// ClusterMutator lets UpdateClusterRMW callers express the intended change
// as a function over the current resource state. Return nil to skip the
// PATCH entirely (no-op update — safe to surface as a success).
type ClusterMutator func(current *Cluster) *PatchClusterRequest

// ErrRMWContention is returned when an RMW helper hits two consecutive 412
// ETag mismatches (i.e., another writer updated the resource each time the
// CLI tried). It wraps the final 412, so it surfaces as cli.ExitConflict.
var ErrRMWContention = errors.New("concurrent update contention; retry")

// UpdateClusterRMW is the safe read-modify-write helper for `cluster update`.
// Flow: GET → mutator → PATCH with If-Match. On 412 (another writer landed
// in the meantime), re-GET and retry once. This matches the pattern
// described in docs/api-client.md.
//
// Passing a nil PatchClusterRequest from the mutator is a no-op — the
// function returns the most recent GET result and no error.
func (c *Client) UpdateClusterRMW(ctx context.Context, name string, mutate ClusterMutator) (*Cluster, error) {
	return updateRMW(ctx,
		func(ctx context.Context) (*Cluster, string, error) { return c.GetCluster(ctx, name) },
		func(ctx context.Context, etag string, req PatchClusterRequest) (*Cluster, string, error) {
			return c.UpdateCluster(ctx, name, etag, req)
		},
		mutate, ErrRMWContention)
}
