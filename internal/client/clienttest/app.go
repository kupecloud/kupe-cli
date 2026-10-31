package clienttest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kupecloud/kupe-cli/internal/client"
)

type appStore[T, C, P any] struct {
	*store[T, C, P]
	errors   *[]error
	confirms *[]string
}

func cloneAppValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	var copy T
	_ = json.Unmarshal(data, &copy)
	return &copy
}
func (s *appStore[T, C, P]) Get(ctx context.Context, name string) (*T, string, error) {
	item, etag, err := s.store.Get(ctx, name)
	return cloneAppValue(item), etag, err
}
func (s *appStore[T, C, P]) Update(ctx context.Context, name, etag string, req P) (*T, string, error) {
	s.f.mu.Lock()
	if len(*s.errors) > 0 {
		err := (*s.errors)[0]
		*s.errors = (*s.errors)[1:]
		if err != nil {
			s.f.record("Update" + s.kind + ":" + name)
			s.f.mu.Unlock()
			return nil, "", err
		}
	}
	s.f.mu.Unlock()
	return s.store.Update(ctx, name, etag, req)
}
func (s *appStore[T, C, P]) UpdateRMW(ctx context.Context, name string, mutate func(*T) *P) (*T, error) {
	return client.UpdateResourceRMW(ctx, func(ctx context.Context) (*T, string, error) { return s.Get(ctx, name) }, func(ctx context.Context, etag string, req P) (*T, string, error) {
		return s.Update(ctx, name, etag, req)
	}, mutate, client.ErrRMWContention)
}
func (s *appStore[T, C, P]) Delete(ctx context.Context, name string) error {
	return s.DeleteConfirmed(ctx, name, name)
}
func (s *appStore[T, C, P]) DeleteConfirmed(ctx context.Context, name, confirm string) error {
	s.f.mu.Lock()
	*s.confirms = append(*s.confirms, confirm)
	s.f.mu.Unlock()
	if confirm != name {
		return &client.APIError{StatusCode: 400, Code: "confirm_mismatch", Message: "confirm must equal the name"}
	}
	return s.store.Delete(ctx, name)
}

// Apps implements the same collection contract as the HTTP app client.
func (f *Fake) Apps() client.AppAPI {
	return &appStore[client.App, client.CreateAppRequest, client.PatchAppRequest]{
		store: &store[client.App, client.CreateAppRequest, client.PatchAppRequest]{f: f, kind: "App", noun: "app", items: &f.AppsByName, getSeq: &f.GetAppSeq, listErr: &f.ListAppsErr, getErr: &f.GetAppErr, createErr: &f.CreateAppErr, updateErr: &f.UpdateAppErr, delErr: &f.DeleteAppErr,
			name: func(req client.CreateAppRequest) string { return req.Name },
			build: func(req client.CreateAppRequest) *client.App {
				f.AppCreates = append(f.AppCreates, req)
				return &client.App{Name: req.Name, UID: "app-uid", Generation: 1, DeletionProtection: req.DeletionProtection == nil || *req.DeletionProtection, Status: &client.AppStatus{Phase: client.PhaseRunning, ObservedGeneration: 1}}
			},
			apply: func(app *client.App, req client.PatchAppRequest) {
				f.AppPatches = append(f.AppPatches, req)
				if req.DeletionProtection != nil {
					app.DeletionProtection = *req.DeletionProtection
				}
				app.Generation++
				app.Status = &client.AppStatus{Phase: client.PhaseRunning, ObservedGeneration: app.Generation}
			},
			refuseDelete: func(app *client.App) error {
				if app.DeletionProtection {
					return protectedApp("app", app.Name)
				}
				return nil
			},
		}, errors: &f.UpdateAppErrors, confirms: &f.AppDeleteConfirms,
	}
}
func protectedApp(noun, name string) error {
	return &client.APIError{StatusCode: 409, Code: "deletion_protected", Message: fmt.Sprintf("%s %q has deletion protection on; turn it off first", noun, name)}
}

// AppEnvironments scopes the in-memory store to its parent app.
func (f *Fake) AppEnvironments(app string) client.AppEnvironmentAPI {
	f.mu.Lock()
	if f.AppEnvironmentsByApp[app] == nil {
		f.AppEnvironmentsByApp[app] = map[string]*client.AppEnvironment{}
	}
	if f.GetAppEnvironmentSeq[app] == nil {
		f.GetAppEnvironmentSeq[app] = map[string][]*client.AppEnvironment{}
	}
	items, seq := f.AppEnvironmentsByApp[app], f.GetAppEnvironmentSeq[app]
	f.mu.Unlock()
	return &appStore[client.AppEnvironment, client.CreateAppEnvironmentRequest, client.PatchAppEnvironmentRequest]{
		store: &store[client.AppEnvironment, client.CreateAppEnvironmentRequest, client.PatchAppEnvironmentRequest]{f: f, kind: "AppEnvironment", noun: "environment", items: &items, getSeq: &seq, listErr: &f.ListAppEnvironmentsErr, getErr: &f.GetAppEnvironmentErr, createErr: &f.CreateAppEnvironmentErr, updateErr: &f.UpdateAppEnvironmentErr, delErr: &f.DeleteAppEnvironmentErr,
			name: func(req client.CreateAppEnvironmentRequest) string { return req.Environment },
			build: func(req client.CreateAppEnvironmentRequest) *client.AppEnvironment {
				f.AppEnvironmentCreates = append(f.AppEnvironmentCreates, req)
				return &client.AppEnvironment{Name: app + "-" + req.Environment, App: app, Environment: req.Environment, Cluster: req.Cluster, UID: "env-uid", Generation: 1, DeploymentRevision: 1, DeletionProtection: req.DeletionProtection == nil || *req.DeletionProtection, Web: req.Web, Secrets: req.Secrets, Domains: req.Domains, Status: &client.AppEnvironmentStatus{Phase: client.PhaseRunning, ObservedGeneration: 1}}
			},
			apply: func(env *client.AppEnvironment, req client.PatchAppEnvironmentRequest) {
				f.AppEnvironmentPatches = append(f.AppEnvironmentPatches, req)
				if req.Web != nil {
					env.Web = req.Web
				}
				if req.Secrets != nil {
					env.Secrets = *req.Secrets
				}
				if req.Domains != nil {
					env.Domains = *req.Domains
				}
				if req.DeletionProtection != nil {
					env.DeletionProtection = *req.DeletionProtection
				}
				env.Generation++
				env.Status = &client.AppEnvironmentStatus{Phase: client.PhaseRunning, ObservedGeneration: env.Generation}
			},
			refuseDelete: func(env *client.AppEnvironment) error {
				if env.DeletionProtection {
					return protectedApp("environment", env.Environment)
				}
				return nil
			},
		}, errors: &f.UpdateAppEnvironmentErrors, confirms: &f.AppEnvironmentDeleteConfirms,
	}
}
