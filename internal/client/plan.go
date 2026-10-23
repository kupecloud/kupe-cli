package client

import (
	"context"
	"net/http"
	"net/url"
)

// Plan is the public catalog entry for a platform tier. Endpoint is
// unauthenticated server-side, but the CLI still sends the Authorization
// header it would for any other call — the server ignores auth on public
// endpoints.
//
// Plan carries no amount: kupe-api's platformFee is not decoded, so no
// output format prints it (no prices in alpha, D-40; kupecloud/kupe-api#42).
// Do not add a price, fee, cost or currency field here.
type Plan struct {
	Name              string                 `json:"name" yaml:"name"`
	DisplayName       string                 `json:"displayName" yaml:"displayName"`
	ResourcePool      *ResourcePool          `json:"resourcePool,omitempty" yaml:"resourcePool,omitempty"`
	ObservabilityPool *PlanObservabilityPool `json:"observabilityPool,omitempty" yaml:"observabilityPool,omitempty"`
	MaxClusters       int64                  `json:"maxClusters,omitempty" yaml:"maxClusters,omitempty"`
	// MaxDatabases is the plan's database limit, 0 meaning unlimited; nil
	// from a kupe-api that predates it or for a plan that does not set one.
	MaxDatabases *int64 `json:"maxDatabases,omitempty" yaml:"maxDatabases,omitempty"`
	// Registry is the plan's container registry allowance; nil from a
	// kupe-api that predates it or for a plan that includes no registry.
	Registry *PlanRegistry `json:"registry,omitempty" yaml:"registry,omitempty"`
}

// PlanRegistry is a plan's container registry allowance. It carries the
// storage cap only, as a quantity ("5Gi"; "0" meaning unlimited).
type PlanRegistry struct {
	MaxStorage string `json:"maxStorage,omitempty" yaml:"maxStorage,omitempty"`
}

// PlanObservabilityPool is the metrics/logs allowance for a plan.
type PlanObservabilityPool struct {
	MaxActiveSeries int64 `json:"maxActiveSeries,omitempty" yaml:"maxActiveSeries,omitempty"`
	LogIngestGB     int64 `json:"logIngestGB,omitempty" yaml:"logIngestGB,omitempty"`
	RetentionDays   int   `json:"retentionDays,omitempty" yaml:"retentionDays,omitempty"`
	MaxReceivers    int   `json:"maxReceivers,omitempty" yaml:"maxReceivers,omitempty"`
}

// plansPath is the base URL for the public plans endpoints. Unlike every
// other resource, this is NOT under /tenants/{tenant}.
const plansPath = "/api/v1/plans"

// ListPlans returns every platform plan.
func (c *Client) ListPlans(ctx context.Context) ([]Plan, error) {
	var resp struct {
		Items []Plan `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, plansPath, nil, &resp)
	return resp.Items, err
}

// GetPlan fetches a single plan by name (e.g. "starter", "pro").
func (c *Client) GetPlan(ctx context.Context, name string) (*Plan, error) {
	var p Plan
	_, err := c.request(ctx, http.MethodGet, plansPath+"/"+url.PathEscape(name), nil, &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
