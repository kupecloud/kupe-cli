package client

import (
	"context"
	"errors"
	"net/http"
)

// Secret mirrors kupe-api's managedSecretResponse. The server stores the
// actual secret material in OpenBao (see kupe-control-operator) and only
// returns metadata: the Vault path pointer plus a list of clusters/
// namespaces the secret is synced to.
type Secret struct {
	Name            string        `json:"name" yaml:"name"`
	SecretPath      string        `json:"secretPath" yaml:"secretPath"`
	Sync            []SyncTarget  `json:"sync,omitempty" yaml:"sync,omitempty"`
	Status          *SecretStatus `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string        `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string        `json:"createdAt" yaml:"createdAt"`
}

// SyncTarget pins a managed secret into a specific cluster + namespace.
// SecretName is optional; if empty, the server defaults to Secret.Name.
type SyncTarget struct {
	Cluster    string `json:"cluster" yaml:"cluster"`
	Namespace  string `json:"namespace" yaml:"namespace"`
	SecretName string `json:"secretName,omitempty" yaml:"secretName,omitempty"`
}

// SecretStatus carries the operator's observed state. Phase values
// (Pending / Active / Degraded / Deleting) mirror the CRD.
type SecretStatus struct {
	Phase string `json:"phase" yaml:"phase"`
}

// CreateSecretRequest is the POST body.
type CreateSecretRequest struct {
	Name       string       `json:"name"`
	SecretPath string       `json:"secretPath"`
	Sync       []SyncTarget `json:"sync,omitempty"`
}

// PatchSecretRequest updates a secret. Only the sync list is mutable via
// the API — secretPath is immutable post-create (the CRD is too, because
// the secret is seeded once into OpenBao).
type PatchSecretRequest struct {
	Sync []SyncTarget `json:"sync"`
}

// SecretMutator mirrors ClusterMutator for the secret RMW helper. Return
// nil to no-op.
type SecretMutator func(current *Secret) *PatchSecretRequest

// ErrSecretRMWContention mirrors ErrRMWContention for secret updates.
var ErrSecretRMWContention = errors.New("secret: concurrent update contention; retry")

// ListSecrets returns every secret in the tenant.
func (c *Client) ListSecrets(ctx context.Context) ([]Secret, error) {
	var resp struct {
		Items []Secret `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("secrets"), nil, &resp)
	return resp.Items, err
}

// GetSecret returns a single secret + its ETag.
func (c *Client) GetSecret(ctx context.Context, name string) (*Secret, string, error) {
	var secret Secret
	etag, err := c.request(ctx, http.MethodGet, c.tenantPath("secrets", name), nil, &secret)
	if err != nil {
		return nil, "", err
	}
	return &secret, etag, nil
}

// CreateSecret posts a new secret.
func (c *Client) CreateSecret(ctx context.Context, req CreateSecretRequest) (*Secret, string, error) {
	var secret Secret
	etag, err := c.request(ctx, http.MethodPost, c.tenantPath("secrets"), req, &secret)
	if err != nil {
		return nil, "", err
	}
	return &secret, etag, nil
}

// UpdateSecret patches a secret with optimistic locking.
func (c *Client) UpdateSecret(ctx context.Context, name, etag string, req PatchSecretRequest) (*Secret, string, error) {
	var secret Secret
	newETag, err := c.requestWithETag(ctx, http.MethodPatch, c.tenantPath("secrets", name), etag, req, &secret)
	if err != nil {
		return nil, "", err
	}
	return &secret, newETag, nil
}

// UpdateSecretRMW is the read-modify-write helper for `secret update`.
// Mirrors UpdateClusterRMW — one 412 retry, then ErrSecretRMWContention.
func (c *Client) UpdateSecretRMW(ctx context.Context, name string, mutate SecretMutator) (*Secret, error) {
	for attempt := 0; attempt < 2; attempt++ {
		current, etag, err := c.GetSecret(ctx, name)
		if err != nil {
			return nil, err
		}
		patch := mutate(current)
		if patch == nil {
			return current, nil
		}
		updated, _, err := c.UpdateSecret(ctx, name, etag, *patch)
		if err == nil {
			return updated, nil
		}
		if !IsPreconditionFailed(err) {
			return nil, err
		}
	}
	return nil, ErrSecretRMWContention
}

// DeleteSecret revokes a secret.
func (c *Client) DeleteSecret(ctx context.Context, name string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("secrets", name), nil, nil)
	return err
}
