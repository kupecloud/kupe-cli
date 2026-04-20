package client

import (
	"context"
	"net/http"
)

// APIKey mirrors kupe-api's apiKeyResponse (see
// kupe-api/internal/server/handler_apikey.go). Key is only populated on the
// CREATE response — never on list or get. It carries the raw `kupe_...` token
// that the API surface returns exactly once.
type APIKey struct {
	ID          string `json:"id" yaml:"id"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	Role        string `json:"role" yaml:"role"`
	CreatedBy   string `json:"createdBy" yaml:"createdBy"`
	ExpiresAt   string `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
	LastUsedAt  string `json:"lastUsedAt,omitempty" yaml:"lastUsedAt,omitempty"`
	CreatedAt   string `json:"createdAt" yaml:"createdAt"`
	Key         string `json:"key,omitempty" yaml:"key,omitempty"`
}

// CreateAPIKeyRequest is the POST body. Role is admin or readonly;
// ExpiresAt is optional RFC3339.
type CreateAPIKeyRequest struct {
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

// API key role constants. Match kupe-api server values.
const (
	RoleAdmin    = "admin"
	RoleReadonly = "readonly"
)

// ListAPIKeys returns all keys for the tenant. Admin-only — the server
// returns 403 for readonly callers, which maps to ExitAuth (3).
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	var resp struct {
		Items []APIKey `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("apikeys"), nil, &resp)
	return resp.Items, err
}

// CreateAPIKey creates a new key and returns the raw `kupe_...` value in
// APIKey.Key — the only place the caller will ever see it. Admin-only.
func (c *Client) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (*APIKey, error) {
	var key APIKey
	_, err := c.request(ctx, http.MethodPost, c.tenantPath("apikeys"), req, &key)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// DeleteAPIKey revokes a key by ID. Idempotent-ish: a repeat delete returns
// IsNotFound.
func (c *Client) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("apikeys", id), nil, nil)
	return err
}
