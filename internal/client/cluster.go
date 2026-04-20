package client

import (
	"context"
	"net/http"
)

// Cluster is the API response shape for a managed cluster. Field names
// mirror kupe-api's transformCluster output — see
// kupe-api/internal/server/handler_cluster.go.
type Cluster struct {
	Name            string           `json:"name" yaml:"name"`
	DisplayName     string           `json:"displayName" yaml:"displayName"`
	Type            string           `json:"type" yaml:"type"`
	Version         string           `json:"version" yaml:"version"`
	Resources       *ClusterResource `json:"resources,omitempty" yaml:"resources,omitempty"`
	Alerts          any              `json:"alerts,omitempty" yaml:"alerts,omitempty"`
	Status          *ClusterStatus   `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string           `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string           `json:"createdAt" yaml:"createdAt"`
}

// ClusterResource is the tenant-facing resource envelope. Quantities follow
// Kubernetes resource-quantity syntax (see
// kupe-control-operator/api/v1alpha1/managedcluster_types.go for the regex).
type ClusterResource struct {
	CPU     string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty" yaml:"memory,omitempty"`
	Storage string `json:"storage,omitempty" yaml:"storage,omitempty"`
}

// ClusterStatus mirrors the observed state published by the operator. Phase
// values: Pending | Provisioning | Running | Upgrading | Degraded | Terminating.
type ClusterStatus struct {
	Phase             string `json:"phase,omitempty" yaml:"phase,omitempty"`
	KubernetesVersion string `json:"kubernetesVersion,omitempty" yaml:"kubernetesVersion,omitempty"`
	Endpoint          string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// CreateClusterRequest is the POST body.
type CreateClusterRequest struct {
	Name        string           `json:"name"`
	DisplayName string           `json:"displayName,omitempty"`
	Type        string           `json:"type"`
	Version     string           `json:"version,omitempty"`
	Resources   *ClusterResource `json:"resources,omitempty"`
	Alerts      any              `json:"alerts,omitempty"`
}

// PatchClusterRequest is the PATCH body — all fields optional; only the
// populated ones are sent to the server.
type PatchClusterRequest struct {
	Version   *string          `json:"version,omitempty"`
	Resources *ClusterResource `json:"resources,omitempty"`
	Alerts    any              `json:"alerts,omitempty"`
}

// Phase helper constants for waiter comparisons. Defined alongside the
// types they describe so command code doesn't have to stringify.
const (
	PhasePending      = "Pending"
	PhaseProvisioning = "Provisioning"
	PhaseRunning      = "Running"
	PhaseUpgrading    = "Upgrading"
	PhaseDegraded     = "Degraded"
	PhaseTerminating  = "Terminating"
)

// ListClusters returns every cluster visible to the tenant.
func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	var resp struct {
		Items []Cluster `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("clusters"), nil, &resp)
	return resp.Items, err
}

// GetCluster returns one cluster by name, along with its ETag.
func (c *Client) GetCluster(ctx context.Context, name string) (*Cluster, string, error) {
	var cluster Cluster
	etag, err := c.request(ctx, http.MethodGet, c.tenantPath("clusters", name), nil, &cluster)
	if err != nil {
		return nil, "", err
	}
	return &cluster, etag, nil
}

// CreateCluster POSTs a new cluster. Returns the server's response plus the
// created resource's ETag for callers that want to do immediate follow-up
// PATCH without another GET.
func (c *Client) CreateCluster(ctx context.Context, req CreateClusterRequest) (*Cluster, string, error) {
	var cluster Cluster
	etag, err := c.request(ctx, http.MethodPost, c.tenantPath("clusters"), req, &cluster)
	if err != nil {
		return nil, "", err
	}
	return &cluster, etag, nil
}

// UpdateCluster PATCHes a cluster with optimistic locking via If-Match.
// Pass etag="" to skip the If-Match header (server-side RV is still used
// internally for consistency — see kupe-api handler_cluster.go).
func (c *Client) UpdateCluster(ctx context.Context, name, etag string, req PatchClusterRequest) (*Cluster, string, error) {
	var cluster Cluster
	newETag, err := c.requestWithETag(ctx, http.MethodPatch, c.tenantPath("clusters", name), etag, req, &cluster)
	if err != nil {
		return nil, "", err
	}
	return &cluster, newETag, nil
}

// DeleteCluster sends DELETE. Returns nil on success (204) or IsNotFound on
// a cluster that was already gone.
func (c *Client) DeleteCluster(ctx context.Context, name string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("clusters", name), nil, nil)
	return err
}
