package client

import (
	"context"
	"net/http"
)

// Cluster access tokens (kupe-api POST/GET/DELETE
// /tenants/{tenant}/clusters/{name}/access-tokens[/{id}] and POST
// .../access-tokens/{id}/token): credentials for automation that runs
// kubectl or a Kubernetes client against one cluster. The token itself is
// returned once, by the collect, to the member or API key that created it;
// list, get and create never carry it.

// ClusterAccessTokenRoleAdmin is the one role v1 tokens have.
const ClusterAccessTokenRoleAdmin = "admin"

// PhaseRevoking is a cluster access token being withdrawn after a revoke.
const PhaseRevoking = "Revoking"

// Codes kupe-api returns for cluster access tokens, lower-cased as
// ErrorCode returns them.
const (
	// ClusterAccessTokenCodeLimitReached: 409 for a token over the
	// cluster's cap (20).
	ClusterAccessTokenCodeLimitReached = "access_token_limit_reached" // #nosec G101 -- an error code, not a credential
	// ClusterAccessTokenCodeExpiryInvalid: 400 for an expiry out of range.
	ClusterAccessTokenCodeExpiryInvalid = "access_token_expiry_invalid" // #nosec G101 -- an error code, not a credential
	// ClusterAccessTokenCodeNotReady: 409 from the collect before the
	// token is issued, or while it changes under the collect; retry.
	ClusterAccessTokenCodeNotReady = "access_token_not_ready" // #nosec G101 -- an error code, not a credential
	// ClusterAccessTokenCodeAlreadyCollected: 410 from the collect of a
	// token served once already.
	ClusterAccessTokenCodeAlreadyCollected = "access_token_already_collected" // #nosec G101 -- an error code, not a credential
	// ClusterAccessTokenCodeFailed: 409 from the collect of a token that
	// could not be issued and never will be.
	ClusterAccessTokenCodeFailed = "access_token_failed" // #nosec G101 -- an error code, not a credential
	// ClusterAccessTokenCodeCollectCreatorOnly: 403 from the collect by
	// anyone but the member or API key that created the token.
	ClusterAccessTokenCodeCollectCreatorOnly = "access_token_collect_creator_only" // #nosec G101 -- an error code, not a credential
)

// ClusterAccessTokenAccess is what a token may do inside the cluster: a
// kupe role.
type ClusterAccessTokenAccess struct {
	Role string `json:"role" yaml:"role"`
}

// ClusterAccessToken is a cluster access token as list and get return it.
// It never holds the token.
type ClusterAccessToken struct {
	// ID is cat- and 16 hex characters.
	ID          string                   `json:"id" yaml:"id"`
	Cluster     string                   `json:"cluster" yaml:"cluster"`
	DisplayName string                   `json:"displayName" yaml:"displayName"`
	Access      ClusterAccessTokenAccess `json:"access" yaml:"access"`
	// CreatedBy is the email of the member who created it, or the id
	// (ak-...) of the API key that did.
	CreatedBy   string                    `json:"createdBy" yaml:"createdBy"`
	CreatedAt   string                    `json:"createdAt" yaml:"createdAt"`
	ExpiresAt   string                    `json:"expiresAt" yaml:"expiresAt"`
	CollectedAt string                    `json:"collectedAt,omitempty" yaml:"collectedAt,omitempty"`
	Collected   bool                      `json:"collected" yaml:"collected"`
	Status      *ClusterAccessTokenStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// ClusterAccessTokenStatus is a token's observed state. Phase: Pending
// until the token is issued (seconds), then Ready; Revoking while it is
// withdrawn; Failed means it can never work.
type ClusterAccessTokenStatus struct {
	Phase      string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Message    string `json:"message,omitempty" yaml:"message,omitempty"`
	Conditions []any  `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// Phase returns the token's phase, "" if unknown.
func (t *ClusterAccessToken) Phase() string {
	if t == nil || t.Status == nil {
		return ""
	}
	return t.Status.Phase
}

// CreateClusterAccessTokenRequest is the create body. A nil ExpiresInDays
// takes the platform default (90 days).
type CreateClusterAccessTokenRequest struct {
	DisplayName   string                   `json:"displayName"`
	Access        ClusterAccessTokenAccess `json:"access"`
	ExpiresInDays *int                     `json:"expiresInDays,omitempty"`
}

// CollectedClusterAccessToken is the collect response: the token and a
// ready-to-use kubeconfig, returned this once.
type CollectedClusterAccessToken struct {
	ID      string `json:"id"`
	Cluster string `json:"cluster"`
	// Token is the bearer token for the cluster's API.
	Token string `json:"token"`
	// Kubeconfig is a kubeconfig (YAML) with the token, the endpoint and
	// the CA, its one context current.
	Kubeconfig string `json:"kubeconfig"`
	Endpoint   string `json:"endpoint"`
	// CertificateAuthority is the cluster's CA, base64-encoded PEM.
	CertificateAuthority string `json:"certificateAuthority"`
	ExpiresAt            string `json:"expiresAt"`
}

// clusterAccessTokenPath is a path under a cluster's access tokens.
func (c *Client) clusterAccessTokenPath(cluster string, segments ...string) string {
	return c.tenantPath(append([]string{"clusters", cluster, "access-tokens"}, segments...)...)
}

// ListClusterAccessTokens returns the cluster's access tokens, newest
// first.
func (c *Client) ListClusterAccessTokens(ctx context.Context, cluster string) ([]ClusterAccessToken, error) {
	var resp struct {
		Items []ClusterAccessToken `json:"items"`
	}
	if _, err := c.request(ctx, http.MethodGet, c.clusterAccessTokenPath(cluster), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetClusterAccessToken returns one of the cluster's access tokens.
func (c *Client) GetClusterAccessToken(ctx context.Context, cluster, id string) (*ClusterAccessToken, error) {
	var t ClusterAccessToken
	if _, err := c.request(ctx, http.MethodGet, c.clusterAccessTokenPath(cluster, id), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateClusterAccessToken creates an access token for the cluster. The
// response carries no token: collect it once its phase is Ready.
func (c *Client) CreateClusterAccessToken(ctx context.Context, cluster string, req CreateClusterAccessTokenRequest) (*ClusterAccessToken, error) {
	var t ClusterAccessToken
	if _, err := c.request(ctx, http.MethodPost, c.clusterAccessTokenPath(cluster), req, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteClusterAccessToken revokes a cluster access token.
func (c *Client) DeleteClusterAccessToken(ctx context.Context, cluster, id string) error {
	_, err := c.request(ctx, http.MethodDelete, c.clusterAccessTokenPath(cluster, id), nil, nil)
	return err
}

// CollectClusterAccessToken returns the token and its kubeconfig, once, to
// the member or API key that created it. The response is never retried or
// kept beyond the return value.
func (c *Client) CollectClusterAccessToken(ctx context.Context, cluster, id string) (*CollectedClusterAccessToken, error) {
	var t CollectedClusterAccessToken
	if _, err := c.request(ctx, http.MethodPost, c.clusterAccessTokenPath(cluster, id, "token"), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
