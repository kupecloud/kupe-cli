package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// VaultEntry is metadata only. There deliberately is no values field.
type VaultEntry struct {
	Path       string   `json:"path" yaml:"path"`
	Version    int64    `json:"version" yaml:"version"`
	CreatedAt  string   `json:"createdAt" yaml:"createdAt"`
	UpdatedAt  string   `json:"updatedAt" yaml:"updatedAt"`
	Deleted    bool     `json:"deleted" yaml:"deleted"`
	References []string `json:"references" yaml:"references"`
}

// VaultWriteResult reports the write and the definitions requested to refresh.
type VaultWriteResult struct {
	Path             string   `json:"path" yaml:"path"`
	Version          int64    `json:"version" yaml:"version"`
	RefreshRequested []string `json:"refreshRequested" yaml:"refreshRequested"`
}

func (c *Client) vaultPath(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return c.tenantPath("vault") + "/" + strings.Join(segments, "/")
}

// GetVaultEntry never asks the API to include secret values.
func (c *Client) GetVaultEntry(ctx context.Context, path string) (*VaultEntry, error) {
	var entry VaultEntry
	_, err := c.request(ctx, http.MethodGet, c.vaultPath(path), nil, &entry)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// PutVaultValues sends exactly one version precondition. PUT transport failures
// and 5xx responses are never replayed by the shared request layer.
func (c *Client) PutVaultValues(ctx context.Context, path string, values map[string]string, expectedVersion int64) (*VaultWriteResult, error) {
	etag := ""
	opts := requestOptions{noRedirect: true}
	if expectedVersion < 0 {
		return nil, fmt.Errorf("expected version must not be negative")
	}
	if expectedVersion == 0 {
		opts.ifNoneMatch = "*"
	} else {
		etag = fmt.Sprintf(`"%d"`, expectedVersion)
	}
	var result VaultWriteResult
	_, err := c.requestWithOptions(ctx, http.MethodPut, c.vaultPath(path), etag, struct {
		Values map[string]string `json:"values"`
	}{values}, &result, opts)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// PurgeVaultEntry asks the server to check the reviewed version before purging.
// It never retries an uncertain outcome. A 429 refusal happens before the API handler runs.
func (c *Client) PurgeVaultEntry(ctx context.Context, path string, version int64) error {
	if version <= 0 {
		return fmt.Errorf("purge version must be positive")
	}
	_, err := c.requestWithOptions(ctx, http.MethodDelete, c.vaultPath(path), fmt.Sprintf(`"%d"`, version), struct {
		Confirm string `json:"confirm"`
	}{path}, nil, requestOptions{noReplay: true, noRedirect: true})
	return err
}
