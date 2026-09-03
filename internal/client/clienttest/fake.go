// Package clienttest provides an in-memory Fake implementing client.Interface
// for use in unit tests. Prefer the Fake over httptest when you're exercising
// command bodies — it keeps tests fast and lets you assert on call counts
// without HTTP-level mocking.
package clienttest

import (
	"context"
	"sync"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// Fake is an in-memory implementation of client.Interface. Set the response
// fields (e.g. TenantResponse, Clusters) or error fields (e.g. TenantErr)
// before handing the Fake to a command under test. Every call increments
// Calls for assertions.
//
// Cluster methods mutate an in-memory map keyed by name, so list/get/create/
// delete behave like a real API from the command's perspective. A caller can
// also override any method by setting a per-method error field.
type Fake struct {
	mu sync.Mutex

	TenantName     string
	TenantResponse *client.Tenant
	TenantETag     string
	TenantErr      error

	// GetTenantSeq scripts successive GetTenant results (e.g. Terminating →
	// gone) for wait loops; each call consumes one entry and the last entry
	// is repeated once exhausted. A nil entry renders as a 404. Empty →
	// TenantResponse/TenantErr apply.
	GetTenantSeq []*client.Tenant

	// DeleteTenantErr forces DeleteTenant to fail. DeleteTenantResponse is
	// the 202 body (defaults to TenantResponse with phase Terminating).
	// DeleteTenantRequests records every body the command sent.
	DeleteTenantErr      error
	DeleteTenantResponse *client.Tenant
	DeleteTenantRequests []client.DeleteTenantRequest

	// Clusters keyed by name. Seed before handing to a test.
	Clusters map[string]*client.Cluster

	// Per-method error overrides — set any of these to force a failure.
	ListClustersErr         error
	GetClusterErr           error
	CreateClusterErr        error
	UpdateClusterErr        error
	DeleteClusterErr        error
	GetClusterKubeconfigErr error

	// Kubeconfigs keyed by cluster name. Seed an entry to make
	// GetClusterKubeconfig return it; absent entries produce a 404.
	Kubeconfigs map[string]*client.ClusterKubeconfig

	// APIKeys keyed by ID. Seed before a list; CreateAPIKey mutates.
	APIKeys map[string]*client.APIKey

	// Per-method error overrides for apikey endpoints.
	ListAPIKeysErr  error
	CreateAPIKeyErr error
	DeleteAPIKeyErr error

	// NextAPIKeyID is used as the ID (and suffix of the raw token) for the
	// next CreateAPIKey call. Tests use this to make creations
	// deterministic. Empty string → "fake-id".
	NextAPIKeyID string

	// Secrets keyed by name.
	Secrets         map[string]*client.Secret
	ListSecretsErr  error
	GetSecretErr    error
	CreateSecretErr error
	UpdateSecretErr error
	DeleteSecretErr error

	// Members keyed by email.
	Members         map[string]*client.Member
	ListMembersErr  error
	AddMemberErr    error
	UpdateMemberErr error
	RemoveMemberErr error

	// Invoices keyed by name (billing period).
	Invoices        map[string]*client.Invoice
	ListInvoicesErr error
	GetInvoiceErr   error

	// Plans keyed by name.
	Plans        map[string]*client.Plan
	ListPlansErr error
	GetPlanErr   error

	// GetClusterSeq lets tests script a sequence of phase transitions for a
	// single cluster name: each GetCluster call for that name consumes the
	// next entry. Once exhausted, the last entry is returned for all
	// subsequent calls. Use to simulate "Pending → Provisioning → Running"
	// over a polling loop. A nil entry is rendered as a 404.
	GetClusterSeq map[string][]*client.Cluster

	// Calls records the method name of every call in order. Useful for
	// asserting a command hits the API exactly once (or not at all).
	Calls []string
}

// New returns a Fake with sensible zero values.
func New() *Fake {
	return &Fake{
		TenantName:    "acme",
		Clusters:      map[string]*client.Cluster{},
		GetClusterSeq: map[string][]*client.Cluster{},
		Kubeconfigs:   map[string]*client.ClusterKubeconfig{},
		APIKeys:       map[string]*client.APIKey{},
		Secrets:       map[string]*client.Secret{},
		Members:       map[string]*client.Member{},
		Invoices:      map[string]*client.Invoice{},
		Plans:         map[string]*client.Plan{},
	}
}

func (f *Fake) record(call string) {
	f.Calls = append(f.Calls, call)
}

// Tenant implements client.Interface.
func (f *Fake) Tenant() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Tenant")
	return f.TenantName
}

// GetTenant implements client.Interface.
func (f *Fake) GetTenant(_ context.Context) (*client.Tenant, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetTenant")
	if len(f.GetTenantSeq) > 0 {
		next := f.GetTenantSeq[0]
		if len(f.GetTenantSeq) > 1 {
			f.GetTenantSeq = f.GetTenantSeq[1:]
		}
		if next == nil {
			return nil, "", &client.APIError{StatusCode: 404, Message: "tenant not found"}
		}
		return next, f.TenantETag, nil
	}
	if f.TenantErr != nil {
		return nil, "", f.TenantErr
	}
	return f.TenantResponse, f.TenantETag, nil
}

// DeleteTenant implements client.Interface. Records the request body so
// tests can assert on confirm/cascade.
func (f *Fake) DeleteTenant(_ context.Context, req client.DeleteTenantRequest) (*client.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteTenant:" + req.Confirm)
	f.DeleteTenantRequests = append(f.DeleteTenantRequests, req)
	if f.DeleteTenantErr != nil {
		return nil, f.DeleteTenantErr
	}
	if f.DeleteTenantResponse != nil {
		return f.DeleteTenantResponse, nil
	}
	name := f.TenantName
	if f.TenantResponse != nil && f.TenantResponse.Name != "" {
		name = f.TenantResponse.Name
	}
	return &client.Tenant{Name: name, Status: &client.TenantStatus{Phase: client.PhaseTerminating}}, nil
}

// ListClusters implements client.Interface.
func (f *Fake) ListClusters(_ context.Context) ([]client.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListClusters")
	if f.ListClustersErr != nil {
		return nil, f.ListClustersErr
	}
	out := make([]client.Cluster, 0, len(f.Clusters))
	for _, c := range f.Clusters {
		out = append(out, *c)
	}
	return out, nil
}

// GetCluster implements client.Interface. Honours GetClusterSeq when a
// sequence is set for the given name, falling through to Clusters
// otherwise.
func (f *Fake) GetCluster(_ context.Context, name string) (*client.Cluster, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetCluster:" + name)
	if f.GetClusterErr != nil {
		return nil, "", f.GetClusterErr
	}
	if seq, ok := f.GetClusterSeq[name]; ok && len(seq) > 0 {
		next := seq[0]
		if len(seq) > 1 {
			f.GetClusterSeq[name] = seq[1:]
		}
		if next == nil {
			return nil, "", &client.APIError{StatusCode: 404, Message: "cluster not found"}
		}
		return next, "etag-fake", nil
	}
	c, ok := f.Clusters[name]
	if !ok {
		return nil, "", &client.APIError{StatusCode: 404, Message: "cluster not found"}
	}
	return c, "etag-fake", nil
}

// CreateCluster implements client.Interface. Persists the request into the
// in-memory map at phase Pending.
func (f *Fake) CreateCluster(_ context.Context, req client.CreateClusterRequest) (*client.Cluster, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateCluster:" + req.Name)
	if f.CreateClusterErr != nil {
		return nil, "", f.CreateClusterErr
	}
	if _, exists := f.Clusters[req.Name]; exists {
		return nil, "", &client.APIError{StatusCode: 409, Message: "cluster already exists"}
	}
	created := &client.Cluster{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Type:        req.Type,
		Version:     req.Version,
		Resources:   req.Resources,
		Alerts:      req.Alerts,
		Status:      &client.ClusterStatus{Phase: client.PhasePending},
	}
	f.Clusters[req.Name] = created
	return created, "etag-fake", nil
}

// UpdateCluster implements client.Interface.
func (f *Fake) UpdateCluster(_ context.Context, name, _ string, req client.PatchClusterRequest) (*client.Cluster, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateCluster:" + name)
	if f.UpdateClusterErr != nil {
		return nil, "", f.UpdateClusterErr
	}
	c, ok := f.Clusters[name]
	if !ok {
		return nil, "", &client.APIError{StatusCode: 404, Message: "cluster not found"}
	}
	if req.Version != nil {
		c.Version = *req.Version
	}
	if req.Resources != nil {
		c.Resources = req.Resources
	}
	return c, "etag-fake", nil
}

// UpdateClusterRMW delegates to the fake's own Get/Update so tests get the
// real RMW control flow without hitting a server.
func (f *Fake) UpdateClusterRMW(ctx context.Context, name string, mutate client.ClusterMutator) (*client.Cluster, error) {
	current, _, err := f.GetCluster(ctx, name)
	if err != nil {
		return nil, err
	}
	patch := mutate(current)
	if patch == nil {
		return current, nil
	}
	updated, _, err := f.UpdateCluster(ctx, name, "", *patch)
	return updated, err
}

// DeleteCluster implements client.Interface.
func (f *Fake) DeleteCluster(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteCluster:" + name)
	if f.DeleteClusterErr != nil {
		return f.DeleteClusterErr
	}
	if _, ok := f.Clusters[name]; !ok {
		return &client.APIError{StatusCode: 404, Message: "cluster not found"}
	}
	delete(f.Clusters, name)
	return nil
}

// ListAPIKeys implements client.Interface.
func (f *Fake) ListAPIKeys(_ context.Context) ([]client.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListAPIKeys")
	if f.ListAPIKeysErr != nil {
		return nil, f.ListAPIKeysErr
	}
	out := make([]client.APIKey, 0, len(f.APIKeys))
	for _, k := range f.APIKeys {
		clone := *k
		clone.Key = "" // never leak raw key on list
		out = append(out, clone)
	}
	return out, nil
}

// CreateAPIKey implements client.Interface. The response always carries a
// raw Key, matching the one-time-reveal contract of the real API.
func (f *Fake) CreateAPIKey(_ context.Context, req client.CreateAPIKeyRequest) (*client.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateAPIKey:" + req.DisplayName)
	if f.CreateAPIKeyErr != nil {
		return nil, f.CreateAPIKeyErr
	}
	id := f.NextAPIKeyID
	if id == "" {
		id = "fake-id"
	}
	key := &client.APIKey{
		ID:          id,
		DisplayName: req.DisplayName,
		Role:        req.Role,
		CreatedBy:   "tester@acme.com",
		ExpiresAt:   req.ExpiresAt,
		CreatedAt:   "2026-04-20T00:00:00Z",
		Key:         "kupe_" + id + "_fakesecret",
	}
	// Persist without the Key — subsequent List calls must not leak it.
	persisted := *key
	persisted.Key = ""
	f.APIKeys[id] = &persisted
	return key, nil
}

// DeleteAPIKey implements client.Interface.
func (f *Fake) DeleteAPIKey(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteAPIKey:" + id)
	if f.DeleteAPIKeyErr != nil {
		return f.DeleteAPIKeyErr
	}
	if _, ok := f.APIKeys[id]; !ok {
		return &client.APIError{StatusCode: 404, Message: "api key not found"}
	}
	delete(f.APIKeys, id)
	return nil
}

// GetClusterKubeconfig implements client.Interface.
func (f *Fake) GetClusterKubeconfig(_ context.Context, name string) (*client.ClusterKubeconfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetClusterKubeconfig:" + name)
	if f.GetClusterKubeconfigErr != nil {
		return nil, f.GetClusterKubeconfigErr
	}
	kc, ok := f.Kubeconfigs[name]
	if !ok {
		return nil, &client.APIError{StatusCode: 404, Message: "cluster not found"}
	}
	return kc, nil
}

// ListSecrets implements client.Interface.
func (f *Fake) ListSecrets(_ context.Context) ([]client.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListSecrets")
	if f.ListSecretsErr != nil {
		return nil, f.ListSecretsErr
	}
	out := make([]client.Secret, 0, len(f.Secrets))
	for _, s := range f.Secrets {
		out = append(out, *s)
	}
	return out, nil
}

// GetSecret implements client.Interface.
func (f *Fake) GetSecret(_ context.Context, name string) (*client.Secret, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetSecret:" + name)
	if f.GetSecretErr != nil {
		return nil, "", f.GetSecretErr
	}
	s, ok := f.Secrets[name]
	if !ok {
		return nil, "", &client.APIError{StatusCode: 404, Message: "secret not found"}
	}
	return s, "etag-fake", nil
}

// CreateSecret implements client.Interface.
func (f *Fake) CreateSecret(_ context.Context, req client.CreateSecretRequest) (*client.Secret, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateSecret:" + req.Name)
	if f.CreateSecretErr != nil {
		return nil, "", f.CreateSecretErr
	}
	if _, exists := f.Secrets[req.Name]; exists {
		return nil, "", &client.APIError{StatusCode: 409, Message: "secret already exists"}
	}
	s := &client.Secret{
		Name:       req.Name,
		SecretPath: req.SecretPath,
		Sync:       req.Sync,
		Status:     &client.SecretStatus{Phase: "Pending"},
	}
	f.Secrets[req.Name] = s
	return s, "etag-fake", nil
}

// UpdateSecret implements client.Interface.
func (f *Fake) UpdateSecret(_ context.Context, name, _ string, req client.PatchSecretRequest) (*client.Secret, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateSecret:" + name)
	if f.UpdateSecretErr != nil {
		return nil, "", f.UpdateSecretErr
	}
	s, ok := f.Secrets[name]
	if !ok {
		return nil, "", &client.APIError{StatusCode: 404, Message: "secret not found"}
	}
	s.Sync = req.Sync
	return s, "etag-fake", nil
}

// UpdateSecretRMW delegates to Get + Update so tests exercise the real
// RMW path.
func (f *Fake) UpdateSecretRMW(ctx context.Context, name string, mutate client.SecretMutator) (*client.Secret, error) {
	current, _, err := f.GetSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	patch := mutate(current)
	if patch == nil {
		return current, nil
	}
	updated, _, err := f.UpdateSecret(ctx, name, "", *patch)
	return updated, err
}

// DeleteSecret implements client.Interface.
func (f *Fake) DeleteSecret(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteSecret:" + name)
	if f.DeleteSecretErr != nil {
		return f.DeleteSecretErr
	}
	if _, ok := f.Secrets[name]; !ok {
		return &client.APIError{StatusCode: 404, Message: "secret not found"}
	}
	delete(f.Secrets, name)
	return nil
}

// ListMembers implements client.Interface.
func (f *Fake) ListMembers(_ context.Context) ([]client.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListMembers")
	if f.ListMembersErr != nil {
		return nil, f.ListMembersErr
	}
	out := make([]client.Member, 0, len(f.Members))
	for _, m := range f.Members {
		out = append(out, *m)
	}
	return out, nil
}

// AddMember implements client.Interface.
func (f *Fake) AddMember(_ context.Context, req client.AddMemberRequest) (*client.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("AddMember:" + req.Email)
	if f.AddMemberErr != nil {
		return nil, f.AddMemberErr
	}
	if _, exists := f.Members[req.Email]; exists {
		return nil, &client.APIError{StatusCode: 409, Message: "member already exists"}
	}
	m := &client.Member{Email: req.Email, Role: req.Role}
	f.Members[req.Email] = m
	return m, nil
}

// UpdateMember implements client.Interface.
func (f *Fake) UpdateMember(_ context.Context, email string, req client.UpdateMemberRequest) (*client.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateMember:" + email)
	if f.UpdateMemberErr != nil {
		return nil, f.UpdateMemberErr
	}
	m, ok := f.Members[email]
	if !ok {
		return nil, &client.APIError{StatusCode: 404, Message: "member not found"}
	}
	m.Role = req.Role
	return m, nil
}

// RemoveMember implements client.Interface.
func (f *Fake) RemoveMember(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("RemoveMember:" + email)
	if f.RemoveMemberErr != nil {
		return f.RemoveMemberErr
	}
	if _, ok := f.Members[email]; !ok {
		return &client.APIError{StatusCode: 404, Message: "member not found"}
	}
	delete(f.Members, email)
	return nil
}

// ListInvoices implements client.Interface.
func (f *Fake) ListInvoices(_ context.Context) ([]client.Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListInvoices")
	if f.ListInvoicesErr != nil {
		return nil, f.ListInvoicesErr
	}
	out := make([]client.Invoice, 0, len(f.Invoices))
	for _, inv := range f.Invoices {
		out = append(out, *inv)
	}
	return out, nil
}

// GetInvoice implements client.Interface.
func (f *Fake) GetInvoice(_ context.Context, name string) (*client.Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetInvoice:" + name)
	if f.GetInvoiceErr != nil {
		return nil, f.GetInvoiceErr
	}
	inv, ok := f.Invoices[name]
	if !ok {
		return nil, &client.APIError{StatusCode: 404, Message: "invoice not found"}
	}
	return inv, nil
}

// ListPlans implements client.Interface.
func (f *Fake) ListPlans(_ context.Context) ([]client.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListPlans")
	if f.ListPlansErr != nil {
		return nil, f.ListPlansErr
	}
	out := make([]client.Plan, 0, len(f.Plans))
	for _, p := range f.Plans {
		out = append(out, *p)
	}
	return out, nil
}

// GetPlan implements client.Interface.
func (f *Fake) GetPlan(_ context.Context, name string) (*client.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetPlan:" + name)
	if f.GetPlanErr != nil {
		return nil, f.GetPlanErr
	}
	p, ok := f.Plans[name]
	if !ok {
		return nil, &client.APIError{StatusCode: 404, Message: "plan not found"}
	}
	return p, nil
}

// Compile-time interface check.
var _ client.Interface = (*Fake)(nil)
