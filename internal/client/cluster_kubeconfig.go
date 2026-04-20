package client

import (
	"context"
	"net/http"
)

// ClusterKubeconfig is the response from GET /clusters/{name}/kubeconfig.
// The server returns only the endpoint and CA — the CLI assembles the full
// kubeconfig YAML locally (see internal/kubeconfig).
//
// 503 is surfaced when the cluster is not yet Running; callers should either
// pair `kubeconfig --merge` with `cluster wait`, or handle IsUnavailable.
type ClusterKubeconfig struct {
	Endpoint             string `json:"endpoint"`
	CertificateAuthority string `json:"certificateAuthority"`
}

// GetClusterKubeconfig fetches the endpoint+CA envelope for a cluster.
func (c *Client) GetClusterKubeconfig(ctx context.Context, name string) (*ClusterKubeconfig, error) {
	var kc ClusterKubeconfig
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("clusters", name, "kubeconfig"), nil, &kc)
	if err != nil {
		return nil, err
	}
	return &kc, nil
}
