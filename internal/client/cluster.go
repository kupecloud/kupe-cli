package client

import (
	"context"
	"net/http"
)

// Cluster is the API response shape for a managed cluster. Field names
// mirror kupe-api's transformCluster output — see
// kupe-api/internal/server/handler_cluster.go.
type Cluster struct {
	Name        string           `json:"name" yaml:"name"`
	DisplayName string           `json:"displayName" yaml:"displayName"`
	Type        string           `json:"type" yaml:"type"`
	Version     string           `json:"version" yaml:"version"`
	Resources   *ClusterResource `json:"resources,omitempty" yaml:"resources,omitempty"`
	Alerts      any              `json:"alerts,omitempty" yaml:"alerts,omitempty"`
	// HighAvailability is the requested HA state. status.HAConfigured + status.HAEnabledAt
	// describe the operational reality.
	HighAvailability bool           `json:"highAvailability,omitempty" yaml:"highAvailability,omitempty"`
	Status           *ClusterStatus `json:"status,omitempty" yaml:"status,omitempty"`
	// Generation is metadata.generation — bumps by 1 on every spec mutation.
	// Pair it with status.observedGeneration to know when the operator has
	// reconciled a PATCH; required because in-place updates (resource limit
	// bumps) don't transition phase, so phase alone isn't a convergence signal.
	Generation      int64  `json:"generation,omitempty" yaml:"generation,omitempty"`
	ResourceVersion string `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string `json:"createdAt" yaml:"createdAt"`
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
	// ObservedGeneration is the metadata.generation the operator last
	// reconciled. Compare against Cluster.Generation to know whether a
	// recent PATCH has been applied.
	ObservedGeneration int64  `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Phase              string `json:"phase,omitempty" yaml:"phase,omitempty"`
	KubernetesVersion  string `json:"kubernetesVersion,omitempty" yaml:"kubernetesVersion,omitempty"`
	Endpoint           string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	// HAConfigured is true once the operator has confirmed both 3/3 apiserver
	// replicas AND 3/3 deployed-etcd replicas are Ready. Etcd readiness is
	// required because the OSS deployed-etcd path runs etcd in its own
	// StatefulSet — quorum loss with healthy apiserver pods still blocks
	// writes. HAEnabledAt is the moment that happened (billing anchor).
	HAConfigured bool   `json:"haConfigured,omitempty" yaml:"haConfigured,omitempty"`
	HAEnabledAt  string `json:"haEnabledAt,omitempty" yaml:"haEnabledAt,omitempty"`
	// HAPhase is the consumer-friendly HA rollup. One of pending,
	// ha-healthy, ha-degraded, ha-unavailable. Empty for non-HA clusters.
	HAPhase string `json:"haPhase,omitempty" yaml:"haPhase,omitempty"`
	// HAReplicasReady / HAReplicasDesired surface the apiserver "N of M" pair
	// used by the printer for `cluster list` / `cluster get` display.
	HAReplicasReady   int32 `json:"haReplicasReady,omitempty" yaml:"haReplicasReady,omitempty"`
	HAReplicasDesired int32 `json:"haReplicasDesired,omitempty" yaml:"haReplicasDesired,omitempty"`
	// HAEtcdReplicasReady / HAEtcdReplicasDesired surface the deployed-etcd
	// "N of M" pair. In the OSS deployed-etcd path etcd runs in its own
	// StatefulSet; a 3/3 CP with a 2/3 etcd is still degraded because
	// etcd quorum loss takes writes offline.
	HAEtcdReplicasReady   int32 `json:"haEtcdReplicasReady,omitempty" yaml:"haEtcdReplicasReady,omitempty"`
	HAEtcdReplicasDesired int32 `json:"haEtcdReplicasDesired,omitempty" yaml:"haEtcdReplicasDesired,omitempty"`
}

// CreateClusterRequest is the POST body.
type CreateClusterRequest struct {
	Name             string           `json:"name"`
	DisplayName      string           `json:"displayName,omitempty"`
	Type             string           `json:"type"`
	Version          string           `json:"version,omitempty"`
	Resources        *ClusterResource `json:"resources,omitempty"`
	Alerts           any              `json:"alerts,omitempty"`
	HighAvailability bool             `json:"highAvailability,omitempty"`
}

// PatchClusterRequest is the PATCH body — all fields optional; only the
// populated ones are sent to the server. HA is intentionally absent —
// it's a create-time-only setting in v1 (see cluster create
// --high-availability); both directions of the toggle are rejected by
// the operator with HA_ENABLE_ON_EXISTING_UNSUPPORTED /
// HA_DISABLE_UNSUPPORTED.
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
