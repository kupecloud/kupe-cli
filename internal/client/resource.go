package client

import (
	"context"
	"net/http"
	"slices"
)

// ResourceAPI is the CRUD surface every managed service shares (clusters,
// databases): T is the resource, C its create body and P its patch body.
// A new service adds its types and one accessor on Interface; the HTTP
// side is resource.
type ResourceAPI[T, C, P any] interface {
	// List returns every resource of the kind in the tenant.
	List(ctx context.Context) ([]T, error)
	// Get returns one resource and its ETag.
	Get(ctx context.Context, name string) (*T, string, error)
	// Create posts a new resource and returns the server's answer and its
	// ETag. Provisioning is asynchronous; poll Get for the phase.
	Create(ctx context.Context, req C) (*T, string, error)
	// Update patches a resource with If-Match optimistic locking; an empty
	// etag skips the header.
	Update(ctx context.Context, name, etag string, req P) (*T, string, error)
	// UpdateRMW is the read-modify-write cycle (see updateRMW): one retry
	// on 412, then ErrRMWContention. A nil patch from mutate is a no-op.
	UpdateRMW(ctx context.Context, name string, mutate func(current *T) *P) (*T, error)
	// Delete requests deletion. A resource already gone is a 404.
	Delete(ctx context.Context, name string) error
}

// resource is the HTTP implementation of ResourceAPI for the collection at
// segments under the tenant path ("clusters", or "databases", DB,
// "bindings").
type resource[T, C, P any] struct {
	c        *Client
	segments []string
}

func newResource[T, C, P any](c *Client, segments ...string) resource[T, C, P] {
	return resource[T, C, P]{c: c, segments: segments}
}

// path is the collection path, or an item's when name is given.
func (r resource[T, C, P]) path(name ...string) string {
	return r.c.tenantPath(append(slices.Clone(r.segments), name...)...)
}

func (r resource[T, C, P]) List(ctx context.Context) ([]T, error) {
	var resp struct {
		Items []T `json:"items"`
	}
	_, err := r.c.request(ctx, http.MethodGet, r.path(), nil, &resp)
	return resp.Items, err
}

func (r resource[T, C, P]) Get(ctx context.Context, name string) (*T, string, error) {
	var item T
	etag, err := r.c.request(ctx, http.MethodGet, r.path(name), nil, &item)
	if err != nil {
		return nil, "", err
	}
	return &item, etag, nil
}

func (r resource[T, C, P]) Create(ctx context.Context, req C) (*T, string, error) {
	var item T
	etag, err := r.c.request(ctx, http.MethodPost, r.path(), req, &item)
	if err != nil {
		return nil, "", err
	}
	return &item, etag, nil
}

func (r resource[T, C, P]) Update(ctx context.Context, name, etag string, req P) (*T, string, error) {
	var item T
	newETag, err := r.c.requestWithETag(ctx, http.MethodPatch, r.path(name), etag, req, &item)
	if err != nil {
		return nil, "", err
	}
	return &item, newETag, nil
}

func (r resource[T, C, P]) UpdateRMW(ctx context.Context, name string, mutate func(current *T) *P) (*T, error) {
	return updateRMW(ctx,
		func(ctx context.Context) (*T, string, error) { return r.Get(ctx, name) },
		func(ctx context.Context, etag string, req P) (*T, string, error) {
			return r.Update(ctx, name, etag, req)
		},
		mutate, ErrRMWContention)
}

func (r resource[T, C, P]) Delete(ctx context.Context, name string) error {
	_, err := r.c.request(ctx, http.MethodDelete, r.path(name), nil, nil)
	return err
}

// Clusters is the cluster collection.
func (c *Client) Clusters() ResourceAPI[Cluster, CreateClusterRequest, PatchClusterRequest] {
	return newResource[Cluster, CreateClusterRequest, PatchClusterRequest](c, "clusters")
}

// Databases is the database collection. A database with deletion
// protection on refuses Delete with 409 DatabaseDeleteCodeProtected.
func (c *Client) Databases() ResourceAPI[Database, CreateDatabaseRequest, PatchDatabaseRequest] {
	return newResource[Database, CreateDatabaseRequest, PatchDatabaseRequest](c, "databases")
}
