package client

import (
	"context"
	"net/http"
)

// Tenant mirrors kupe-api's tenantResponse (see kupe-api swagger_models.go).
// The status fields (pool, allocated, usage) are modelled explicitly so the
// CLI's detail view can extract them without any type-asserting through an
// `any` tree — tradeoff is the shapes must stay in sync with the server.
type Tenant struct {
	Name                string        `json:"name" yaml:"name"`
	DisplayName         string        `json:"displayName" yaml:"displayName"`
	ContactEmail        string        `json:"contactEmail" yaml:"contactEmail"`
	Plan                string        `json:"plan" yaml:"plan"`
	EnforceMetricsLimit bool          `json:"enforceMetricsLimit" yaml:"enforceMetricsLimit"`
	EnforceLogLimit     bool          `json:"enforceLogLimit" yaml:"enforceLogLimit"`
	ResourceVersion     string        `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt           string        `json:"createdAt" yaml:"createdAt"`
	Status              *TenantStatus `json:"status,omitempty" yaml:"status,omitempty"`
	Members             []Member      `json:"members,omitempty" yaml:"members,omitempty"`
}

// TenantStatus is the operator-populated status block. All nested pointers
// can be nil if the operator hasn't populated them yet (new tenant, usage
// backfill pending).
type TenantStatus struct {
	Phase              string              `json:"phase,omitempty" yaml:"phase,omitempty"`
	ClusterCount       int64               `json:"clusterCount,omitempty" yaml:"clusterCount,omitempty"`
	AllocatedResources *ResourcePool       `json:"allocatedResources,omitempty" yaml:"allocatedResources,omitempty"`
	PoolResources      *ResourcePool       `json:"poolResources,omitempty" yaml:"poolResources,omitempty"`
	CurrentUsage       *TenantCurrentUsage `json:"currentUsage,omitempty" yaml:"currentUsage,omitempty"`
}

// ResourcePool is the CPU/Memory/Storage triple used both for the tenant's
// plan allocation and for current cluster-level allocation.
type ResourcePool struct {
	CPU     string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty" yaml:"memory,omitempty"`
	Storage string `json:"storage,omitempty" yaml:"storage,omitempty"`
}

// TenantCurrentUsage is the ongoing-period usage snapshot: periods, states
// and quantities. It carries no amount, so no output format prints one (no
// prices in alpha, D-40; kupecloud/kupe-api#42); kupe-api does not serve
// them either. Do not add a price, cost, total, credit or currency field.
type TenantCurrentUsage struct {
	PeriodStart         string                           `json:"periodStart,omitempty" yaml:"periodStart,omitempty"`
	PeriodEnd           string                           `json:"periodEnd,omitempty" yaml:"periodEnd,omitempty"`
	UpdatedAt           string                           `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
	PlanChangedInPeriod bool                             `json:"planChangedInPeriod,omitempty" yaml:"planChangedInPeriod,omitempty"`
	Stale               bool                             `json:"stale,omitempty" yaml:"stale,omitempty"`
	Error               string                           `json:"error,omitempty" yaml:"error,omitempty"`
	Compute             *TenantCurrentUsageCompute       `json:"compute,omitempty" yaml:"compute,omitempty"`
	Observability       *TenantCurrentUsageObservability `json:"observability,omitempty" yaml:"observability,omitempty"`
}

// TenantCurrentUsageCompute is the compute portion of current-period usage.
type TenantCurrentUsageCompute struct {
	CPUCoreHours    string `json:"cpuCoreHours,omitempty" yaml:"cpuCoreHours,omitempty"`
	MemoryGiBHours  string `json:"memoryGiBHours,omitempty" yaml:"memoryGiBHours,omitempty"`
	StorageGiBHours string `json:"storageGiBHours,omitempty" yaml:"storageGiBHours,omitempty"`
}

// TenantCurrentUsageObservability is metrics + logs usage.
type TenantCurrentUsageObservability struct {
	CurrentActiveSeries     int64  `json:"currentActiveSeries,omitempty" yaml:"currentActiveSeries,omitempty"`
	BillableActiveSeriesP95 int64  `json:"billableActiveSeriesP95,omitempty" yaml:"billableActiveSeriesP95,omitempty"`
	LogIngestBytes          int64  `json:"logIngestBytes,omitempty" yaml:"logIngestBytes,omitempty"`
	LogIngestGB             string `json:"logIngestGB,omitempty" yaml:"logIngestGB,omitempty"`
}

// GetTenant returns the tenant this client is scoped to. Also serves as the
// "is this token valid?" probe used by auth login / whoami.
func (c *Client) GetTenant(ctx context.Context) (*Tenant, string, error) {
	var t Tenant
	etag, err := c.request(ctx, http.MethodGet, c.tenantPath(), nil, &t)
	if err != nil {
		return nil, "", err
	}
	return &t, etag, nil
}

// DeleteTenantRequest is the DELETE /api/v1/tenants/{tenant} body. Confirm
// must equal the tenant name (typed-name confirmation, enforced server-side
// as a 400). Cascade asks kupe-api to delete the tenant's clusters first;
// without it a tenant that still has non-Terminating clusters is refused
// with 409 clusters_exist. Cascade never deletes databases or the registry:
// a tenant that has any is refused with 409 databases_exist or
// registries_exist either way.
type DeleteTenantRequest struct {
	Confirm string `json:"confirm"`
	Cascade bool   `json:"cascade,omitempty"`
}

// Canonical error codes kupe-api returns from DELETE /tenants/{tenant}.
// Compare via ErrorCode(err) — it lower-cases so a server that capitalises
// the codes still matches.
const (
	// TenantDeleteCodeOwnerRequired: 403 — caller is not spec.contactEmail,
	// or authenticated with an API key rather than an OIDC token.
	TenantDeleteCodeOwnerRequired = "owner_required"
	// TenantDeleteCodeClustersExist: 409 — active clusters and no cascade.
	TenantDeleteCodeClustersExist = "clusters_exist"
	// TenantDeleteCodeDatabasesExist: 409 — the tenant still has databases.
	// Cascade never deletes them, so this is returned with or without it.
	TenantDeleteCodeDatabasesExist = "databases_exist"
	// TenantDeleteCodeRegistriesExist: 409 — the tenant still has a
	// registry. Cascade never deletes it, so this is returned with or
	// without it.
	TenantDeleteCodeRegistriesExist = "registries_exist"
	// TenantDeleteCodeAlreadyTerminating: 409 — deletionTimestamp already set.
	TenantDeleteCodeAlreadyTerminating = "already_terminating"
)

// DeleteTenant sends DELETE /api/v1/tenants/{tenant}. On 202 the returned
// tenant carries status.phase=Terminating; GetTenant keeps reporting that
// phase until the CR is gone, then 404s. Errors: 400 bad confirm (IsValidation),
// 403 owner_required (IsForbidden), 404 (IsNotFound), 409 clusters_exist /
// databases_exist / registries_exist / already_terminating (IsConflict +
// ErrorCode), 429 (IsRateLimited).
func (c *Client) DeleteTenant(ctx context.Context, req DeleteTenantRequest) (*Tenant, error) {
	var t Tenant
	if _, err := c.request(ctx, http.MethodDelete, c.tenantPath(), req, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
