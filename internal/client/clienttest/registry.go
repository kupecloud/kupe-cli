package clienttest

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// defaultPushSecretName is the platform's default push Secret name.
const defaultPushSecretName = "kupe-registry-push" // #nosec G101 -- a Secret's name, not a credential

// FakeRegistryHost is the registry host the fake's tokens and sessions are
// for.
const FakeRegistryHost = "registry.kupe.cloud"

// recordRegistry records which registry name a call addressed, so tests can
// assert every command uses client.RegistryName.
func (f *Fake) recordRegistry(call, registry string, detail ...string) {
	f.record(strings.Join(append([]string{call, registry}, detail...), ":"))
}

// registryMissing is kupe-api's 404 for a registry that does not exist.
func registryMissing(name string) error {
	return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("registry %q not found", name)}
}

// requireRegistry is a 404 unless the registry exists, as every registry
// sub-resource route answers.
func (f *Fake) requireRegistry(registry string) error {
	if _, ok := f.RegistriesByName[registry]; !ok {
		return registryMissing(registry)
	}
	return nil
}

// Registries implements client.Interface. A created registry starts
// Pending with deletion protection on unless the request turns it off; a
// protected registry is refused delete with the API's 409
// deletion_protected.
func (f *Fake) Registries() client.ResourceAPI[client.Registry, client.CreateRegistryRequest, client.PatchRegistryRequest] {
	return &store[client.Registry, client.CreateRegistryRequest, client.PatchRegistryRequest]{
		f: f, kind: "Registry", noun: "registry",
		items: &f.RegistriesByName, getSeq: &f.GetRegistrySeq,
		listErr: &f.ListRegistriesErr, getErr: &f.GetRegistryErr, createErr: &f.CreateRegistryErr,
		updateErr: &f.UpdateRegistryErr, delErr: &f.DeleteRegistryErr,
		name: func(req client.CreateRegistryRequest) string { return req.Name },
		build: func(req client.CreateRegistryRequest) *client.Registry {
			f.RegistryCreates = append(f.RegistryCreates, req)
			return &client.Registry{
				Name:               req.Name,
				StorageLimit:       req.StorageLimit,
				DeletionProtection: req.DeletionProtection == nil || *req.DeletionProtection,
				Status:             &client.RegistryStatus{Phase: client.PhasePending, Endpoint: FakeRegistryHost + "/" + f.TenantName},
			}
		},
		apply: func(r *client.Registry, patch client.PatchRegistryRequest) {
			f.RegistryPatches = append(f.RegistryPatches, patch)
			if patch.StorageLimit != nil {
				r.StorageLimit = *patch.StorageLimit
			}
			if patch.DeletionProtection != nil {
				r.DeletionProtection = *patch.DeletionProtection
			}
		},
		refuseDelete: func(r *client.Registry) error {
			if !r.DeletionProtection {
				return nil
			}
			return &client.APIError{
				StatusCode: 409,
				Code:       client.RegistryCodeDeletionProtected,
				Message:    `registry "registry" has deletionProtection enabled: set it to false before deleting`,
			}
		},
	}
}

// RegistryBindings implements client.Interface. The call records
// "RegistryBindings:<registry>"; created connections start Pending.
func (f *Fake) RegistryBindings(registry string) client.ResourceAPI[client.RegistryBinding, client.CreateRegistryBindingRequest, client.PatchRegistryBindingRequest] {
	f.mu.Lock()
	f.recordRegistry("RegistryBindings", registry)
	f.mu.Unlock()
	return &store[client.RegistryBinding, client.CreateRegistryBindingRequest, client.PatchRegistryBindingRequest]{
		f: f, kind: "RegistryBinding", noun: "cluster connection",
		items: &f.RegistryBindingsByName, getSeq: &f.GetRegistryBindingSeq,
		listErr: &f.ListRegistryBindingsErr, getErr: &f.GetRegistryBindingErr, createErr: &f.CreateRegistryBindingErr,
		updateErr: &f.UpdateRegistryBindingErr, delErr: &f.DeleteRegistryBindingErr,
		name: func(req client.CreateRegistryBindingRequest) string { return req.Name },
		build: func(req client.CreateRegistryBindingRequest) *client.RegistryBinding {
			f.RegistryBindingCreates = append(f.RegistryBindingCreates, req)
			secretName := req.SecretName
			if secretName == "" {
				secretName = defaultPushSecretName
			}
			return &client.RegistryBinding{
				Name:           req.Name,
				Registry:       registry,
				Cluster:        req.Cluster,
				PushNamespaces: slices.Clone(req.PushNamespaces),
				SecretName:     secretName,
				Status:         &client.RegistryBindingStatus{Phase: client.PhasePending},
			}
		},
		apply: func(b *client.RegistryBinding, patch client.PatchRegistryBindingRequest) {
			f.RegistryBindingPatches = append(f.RegistryBindingPatches, patch)
			if patch.PushNamespaces != nil {
				b.PushNamespaces = slices.Clone(*patch.PushNamespaces)
			}
		},
	}
}

// RotateRegistryBinding implements client.Interface. The call records
// "RotateRegistryBinding:<registry>:<binding>".
func (f *Fake) RotateRegistryBinding(_ context.Context, registry, binding string) (*client.RegistryBindingRotation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("RotateRegistryBinding", registry, binding)
	if f.RotateRegistryBindingErr != nil {
		return nil, f.RotateRegistryBindingErr
	}
	if _, ok := f.RegistryBindingsByName[binding]; !ok {
		return nil, &client.APIError{StatusCode: 404, Message: fmt.Sprintf("cluster connection %q not found", binding)}
	}
	requested := f.NextRotation
	if requested == "" {
		requested = "2026-10-22T12:00:00Z"
	}
	return &client.RegistryBindingRotation{Binding: binding, RequestedAt: requested}, nil
}

// page cuts items into the page asked for (defaults page 1, 25 a page).
func page[T any](items []T, pageNo, pageSize int) *client.Page[T] {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	start := min((pageNo-1)*pageSize, len(items))
	end := min(start+pageSize, len(items))
	return &client.Page[T]{Items: slices.Clone(items[start:end]), Page: pageNo, PageSize: pageSize, Total: len(items)}
}

// ListRepositories implements client.Interface: Search keeps names that
// contain it. Records "ListRepositories:<registry>" and the options.
func (f *Fake) ListRepositories(_ context.Context, registry string, opts client.ListRepositoriesOptions) (*client.Page[client.Repository], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("ListRepositories", registry)
	f.ListRepositoriesCalls = append(f.ListRepositoriesCalls, opts)
	if f.ListRepositoriesErr != nil {
		return nil, f.ListRepositoriesErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	var kept []client.Repository
	for _, r := range f.Repositories {
		if strings.Contains(r.Name, opts.Search) {
			kept = append(kept, r)
		}
	}
	return page(kept, opts.Page, opts.PageSize), nil
}

func repositoryMissing(name string) error {
	return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("repository %q not found", name)}
}

// GetRepository implements client.Interface.
func (f *Fake) GetRepository(_ context.Context, registry, repository string) (*client.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("GetRepository", registry, repository)
	if f.GetRepositoryErr != nil {
		return nil, f.GetRepositoryErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	for _, r := range f.Repositories {
		if r.Name == repository {
			return &r, nil
		}
	}
	return nil, repositoryMissing(repository)
}

// DeleteRepository implements client.Interface.
func (f *Fake) DeleteRepository(_ context.Context, registry, repository string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("DeleteRepository", registry, repository)
	if f.DeleteRepositoryErr != nil {
		return f.DeleteRepositoryErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return err
	}
	for i, r := range f.Repositories {
		if r.Name == repository {
			f.Repositories = slices.Delete(f.Repositories, i, i+1)
			delete(f.Images, repository)
			return nil
		}
	}
	return repositoryMissing(repository)
}

// ListImages implements client.Interface: Tagged keeps tagged (true) or
// untagged (false) images. Records "ListImages:<registry>:<repository>"
// and the options.
func (f *Fake) ListImages(_ context.Context, registry, repository string, opts client.ListImagesOptions) (*client.Page[client.Image], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("ListImages", registry, repository)
	f.ListImagesCalls = append(f.ListImagesCalls, opts)
	if f.ListImagesErr != nil {
		return nil, f.ListImagesErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	images, ok := f.Images[repository]
	if !ok {
		return nil, repositoryMissing(repository)
	}
	var kept []client.Image
	for _, img := range images {
		if opts.Tagged == nil || *opts.Tagged == (len(img.Tags) > 0) {
			kept = append(kept, img)
		}
	}
	return page(kept, opts.Page, opts.PageSize), nil
}

// findImage returns the index of the image with digest or tag reference.
func (f *Fake) findImage(repository, reference string) int {
	for i, img := range f.Images[repository] {
		if img.Digest == reference {
			return i
		}
		for _, tag := range img.Tags {
			if tag.Name == reference {
				return i
			}
		}
	}
	return -1
}

func imageMissing(repository, reference string) error {
	return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("image %q not found in repository %q", reference, repository)}
}

// GetImage implements client.Interface.
func (f *Fake) GetImage(_ context.Context, registry, repository, reference string) (*client.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("GetImage", registry, repository, reference)
	if f.GetImageErr != nil {
		return nil, f.GetImageErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	i := f.findImage(repository, reference)
	if i < 0 {
		return nil, imageMissing(repository, reference)
	}
	img := f.Images[repository][i]
	return &img, nil
}

// DeleteImage implements client.Interface. A tag is refused as the API
// refuses it.
func (f *Fake) DeleteImage(_ context.Context, registry, repository, digest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("DeleteImage", registry, repository, digest)
	if f.DeleteImageErr != nil {
		return f.DeleteImageErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return err
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return &client.APIError{StatusCode: 400, Message: "delete an image by digest; to remove a tag use DELETE .../tags/{tag}"}
	}
	i := f.findImage(repository, digest)
	if i < 0 {
		return imageMissing(repository, digest)
	}
	f.Images[repository] = slices.Delete(f.Images[repository], i, i+1)
	return nil
}

// DeleteTag implements client.Interface.
func (f *Fake) DeleteTag(_ context.Context, registry, repository, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("DeleteTag", registry, repository, tag)
	if f.DeleteTagErr != nil {
		return f.DeleteTagErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return err
	}
	for i, img := range f.Images[repository] {
		for j, t := range img.Tags {
			if t.Name == tag {
				f.Images[repository][i].Tags = slices.Delete(slices.Clone(img.Tags), j, j+1)
				return nil
			}
		}
	}
	return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("tag %q not found in repository %q", tag, repository)}
}

// ListRegistryTokens implements client.Interface; kind narrows the list.
// Records "ListRegistryTokens:<registry>:<kind>".
func (f *Fake) ListRegistryTokens(_ context.Context, registry, kind string) ([]client.RegistryToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("ListRegistryTokens", registry, kind)
	if f.ListRegistryTokensErr != nil {
		return nil, f.ListRegistryTokensErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	out := make([]client.RegistryToken, 0, len(f.RegistryTokens))
	for _, t := range f.RegistryTokens {
		if kind == "" || t.Kind == kind {
			out = append(out, *t)
		}
	}
	slices.SortFunc(out, func(a, b client.RegistryToken) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func tokenMissing(id string) error {
	return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("access token %q not found", id)}
}

// GetRegistryToken implements client.Interface. Honours
// GetRegistryTokenSeq (a nil entry is a 404).
func (f *Fake) GetRegistryToken(_ context.Context, registry, id string) (*client.RegistryToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("GetRegistryToken", registry, id)
	if f.GetRegistryTokenErr != nil {
		return nil, f.GetRegistryTokenErr
	}
	t, ok := popSeq(f.GetRegistryTokenSeq, id)
	if !ok {
		t = f.RegistryTokens[id]
	}
	if t == nil {
		return nil, tokenMissing(id)
	}
	clone := *t
	return &clone, nil
}

// issue stores a new token or session and returns its create response.
func (f *Fake) issue(kind, access, displayName string) *client.CreatedRegistryToken {
	id := f.NextRegistryTokenID
	if id == "" {
		id = "rt-0123456789abcdef"
	}
	short := "tok-"
	creator := "tester@acme.com"
	if kind == client.RegistryTokenKindSession {
		short = "cli-"
		if f.SessionCreator != "" {
			creator = f.SessionCreator
		}
	}
	username := "kupe+" + f.TenantName + "+" + short + strings.TrimPrefix(id, "rt-")
	phase := client.PhaseReady
	if f.CreatedRegistryTokenPhase != "" {
		phase = f.CreatedRegistryTokenPhase
	}
	stored := &client.RegistryToken{
		ID: id, DisplayName: displayName, Kind: kind, Access: access, CreatedBy: creator,
		CreatedAt: "2026-10-22T10:00:00Z", ExpiresAt: "2026-10-22T22:00:00Z",
		Status: &client.RegistryTokenStatus{Phase: phase},
	}
	if phase == client.PhaseReady {
		stored.Username = username
	}
	f.RegistryTokens[id] = stored
	created := &client.CreatedRegistryToken{RegistryToken: *stored, Registry: FakeRegistryHost, Secret: "kupecr_fake" + strings.TrimPrefix(id, "rt-")}
	created.Username = username
	created.Status = &client.RegistryTokenStatus{}
	return created
}

// CreateRegistryToken implements client.Interface. Records
// "CreateRegistryToken:<registry>:<displayName>" and the body.
func (f *Fake) CreateRegistryToken(_ context.Context, registry string, req client.CreateRegistryTokenRequest) (*client.CreatedRegistryToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("CreateRegistryToken", registry, req.DisplayName)
	f.RegistryTokenCreates = append(f.RegistryTokenCreates, req)
	if f.CreateRegistryTokenErr != nil {
		return nil, f.CreateRegistryTokenErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	created := f.issue(client.RegistryTokenKindToken, req.Access, req.DisplayName)
	created.ExpiresAt = "2027-01-20T10:00:00Z"
	f.RegistryTokens[created.ID].ExpiresAt = created.ExpiresAt
	return created, nil
}

// DeleteRegistryToken implements client.Interface.
func (f *Fake) DeleteRegistryToken(_ context.Context, registry, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("DeleteRegistryToken", registry, id)
	if f.DeleteRegistryTokenErr != nil {
		return f.DeleteRegistryTokenErr
	}
	if _, ok := f.RegistryTokens[id]; !ok {
		return tokenMissing(id)
	}
	delete(f.RegistryTokens, id)
	return nil
}

// CreateRegistrySession implements client.Interface: a push session from
// "kupe registry login". Records "CreateRegistrySession:<registry>".
func (f *Fake) CreateRegistrySession(_ context.Context, registry string) (*client.CreatedRegistryToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRegistry("CreateRegistrySession", registry)
	if f.CreateRegistrySessionErr != nil {
		return nil, f.CreateRegistrySessionErr
	}
	if err := f.requireRegistry(registry); err != nil {
		return nil, err
	}
	return f.issue(client.RegistryTokenKindSession, client.RegistryAccessPush, "kupe registry login"), nil
}
