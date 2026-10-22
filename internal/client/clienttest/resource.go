package clienttest

import (
	"context"
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// store is the in-memory client.ResourceAPI behind one service. Its fields
// point at the Fake's own exported fields (Clusters, GetClusterSeq,
// CreateClusterErr, ...), so tests seed it and force failures through the
// Fake exactly as before, and calls are recorded as "GetCluster:NAME",
// "ListClusters", and so on.
type store[T, C, P any] struct {
	f *Fake
	// kind names the calls ("Cluster" records "GetCluster:NAME").
	kind string

	items                                         *map[string]*T
	getSeq                                        *map[string][]*T
	listErr, getErr, createErr, updateErr, delErr *error
	// getErrOnce, when set and non-nil, fails the next Get and is cleared.
	getErrOnce *error

	// name and build turn a create body into the stored resource.
	name  func(C) string
	build func(C) *T
	// apply merges a patch into the stored resource.
	apply func(*T, P)
	// refuseDelete, when set, refuses deleting a stored resource (a
	// database's deletion protection).
	refuseDelete func(*T) error
	// noun, when set, is the resource as kupe-api's 404 names it
	// (`registry "registry" not found`); otherwise "<kind> not found".
	noun string
}

func (s *store[T, C, P]) notFound(name string) error {
	if s.noun != "" {
		return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("%s %q not found", s.noun, name)}
	}
	return &client.APIError{StatusCode: 404, Message: strings.ToLower(s.kind) + " not found"}
}

func (s *store[T, C, P]) List(_ context.Context) ([]T, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.record("List" + s.kind + "s")
	if *s.listErr != nil {
		return nil, *s.listErr
	}
	out := make([]T, 0, len(*s.items))
	for _, item := range *s.items {
		out = append(out, *item)
	}
	return out, nil
}

// Get honours the scripted sequence for name when one is set (a nil entry
// is a 404), falling through to the stored items otherwise.
func (s *store[T, C, P]) Get(_ context.Context, name string) (*T, string, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.record("Get" + s.kind + ":" + name)
	if *s.getErr != nil {
		return nil, "", *s.getErr
	}
	if s.getErrOnce != nil && *s.getErrOnce != nil {
		err := *s.getErrOnce
		*s.getErrOnce = nil
		return nil, "", err
	}
	item, ok := popSeq(*s.getSeq, name)
	if !ok {
		item = (*s.items)[name]
	}
	if item == nil {
		return nil, "", s.notFound(name)
	}
	return item, "etag-fake", nil
}

func (s *store[T, C, P]) Create(_ context.Context, req C) (*T, string, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	name := s.name(req)
	s.f.record("Create" + s.kind + ":" + name)
	if *s.createErr != nil {
		return nil, "", *s.createErr
	}
	if _, exists := (*s.items)[name]; exists {
		return nil, "", &client.APIError{StatusCode: 409, Message: strings.ToLower(s.kind) + " already exists"}
	}
	item := s.build(req)
	(*s.items)[name] = item
	return item, "etag-fake", nil
}

func (s *store[T, C, P]) Update(_ context.Context, name, _ string, req P) (*T, string, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.record("Update" + s.kind + ":" + name)
	if *s.updateErr != nil {
		return nil, "", *s.updateErr
	}
	item, ok := (*s.items)[name]
	if !ok {
		return nil, "", s.notFound(name)
	}
	s.apply(item, req)
	return item, "etag-fake", nil
}

// UpdateRMW delegates to the store's own Get and Update, so tests get the
// real read-modify-write control flow without a server.
func (s *store[T, C, P]) UpdateRMW(ctx context.Context, name string, mutate func(*T) *P) (*T, error) {
	current, _, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	patch := mutate(current)
	if patch == nil {
		return current, nil
	}
	updated, _, err := s.Update(ctx, name, "", *patch)
	return updated, err
}

func (s *store[T, C, P]) Delete(_ context.Context, name string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.record("Delete" + s.kind + ":" + name)
	if *s.delErr != nil {
		return *s.delErr
	}
	item, ok := (*s.items)[name]
	if !ok {
		return s.notFound(name)
	}
	if s.refuseDelete != nil {
		if err := s.refuseDelete(item); err != nil {
			return err
		}
	}
	delete(*s.items, name)
	return nil
}

// Clusters implements client.Interface. Created clusters start Pending.
func (f *Fake) Clusters() client.ResourceAPI[client.Cluster, client.CreateClusterRequest, client.PatchClusterRequest] {
	return &store[client.Cluster, client.CreateClusterRequest, client.PatchClusterRequest]{
		f: f, kind: "Cluster",
		items: &f.ClustersByName, getSeq: &f.GetClusterSeq,
		listErr: &f.ListClustersErr, getErr: &f.GetClusterErr, createErr: &f.CreateClusterErr,
		updateErr: &f.UpdateClusterErr, delErr: &f.DeleteClusterErr,
		name: func(req client.CreateClusterRequest) string { return req.Name },
		build: func(req client.CreateClusterRequest) *client.Cluster {
			return &client.Cluster{
				Name:        req.Name,
				DisplayName: req.DisplayName,
				Type:        req.Type,
				Version:     req.Version,
				Resources:   req.Resources,
				Alerts:      req.Alerts,
				Status:      &client.ClusterStatus{Phase: client.PhasePending},
			}
		},
		apply: func(c *client.Cluster, req client.PatchClusterRequest) {
			if req.Version != nil {
				c.Version = *req.Version
			}
			if req.Resources != nil {
				c.Resources = req.Resources
			}
		},
	}
}

// Databases implements client.Interface. Created databases start Pending
// with the API's defaults (deletion protection on); a protected database is
// refused delete with the API's 409 deletion_protected.
func (f *Fake) Databases() client.ResourceAPI[client.Database, client.CreateDatabaseRequest, client.PatchDatabaseRequest] {
	return &store[client.Database, client.CreateDatabaseRequest, client.PatchDatabaseRequest]{
		f: f, kind: "Database",
		items: &f.DatabasesByName, getSeq: &f.GetDatabaseSeq,
		listErr: &f.ListDatabasesErr, getErr: &f.GetDatabaseErr, getErrOnce: &f.GetDatabaseErrOnce, createErr: &f.CreateDatabaseErr,
		updateErr: &f.UpdateDatabaseErr, delErr: &f.DeleteDatabaseErr,
		name: func(req client.CreateDatabaseRequest) string { return req.Name },
		build: func(req client.CreateDatabaseRequest) *client.Database {
			return &client.Database{
				Name:               req.Name,
				Engine:             req.Engine,
				Version:            req.Version,
				Resources:          req.Resources,
				HighAvailability:   req.HighAvailability,
				DeletionProtection: req.DeletionProtection == nil || *req.DeletionProtection,
				Status:             &client.DatabaseStatus{Phase: client.PhasePending},
			}
		},
		apply: applyDatabasePatch,
		refuseDelete: func(db *client.Database) error {
			if !db.DeletionProtection {
				return nil
			}
			return &client.APIError{
				StatusCode: 409,
				Code:       client.DatabaseDeleteCodeProtected,
				Message:    "database has deletionProtection enabled: set it to false before deleting",
			}
		},
	}
}
