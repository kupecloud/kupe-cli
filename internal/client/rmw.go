package client

import (
	"context"
	"fmt"
)

// updateRMW is the read-modify-write cycle behind every *RMW helper: GET the
// current resource and its ETag, ask mutate for a patch, PATCH with
// If-Match. A 412 means another writer landed in between, so the cycle
// re-reads and retries once. A second 412 returns contention joined with
// that 412, so callers can match the sentinel and the exit code still maps
// to "conflict". A nil patch from mutate is a no-op that returns the
// current resource without a PATCH.
func updateRMW[T, P any](
	ctx context.Context,
	get func(context.Context) (*T, string, error),
	patch func(ctx context.Context, etag string, req P) (*T, string, error),
	mutate func(*T) *P,
	contention error,
) (*T, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		current, etag, err := get(ctx)
		if err != nil {
			return nil, err
		}
		req := mutate(current)
		if req == nil {
			return current, nil
		}
		updated, _, err := patch(ctx, etag, *req)
		if err == nil {
			return updated, nil
		}
		if !IsPreconditionFailed(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w: %w", contention, lastErr)
}
