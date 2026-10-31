package clienttest

import (
	"context"
	"slices"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// GetVaultEntry implements client.Interface without exposing values.
func (f *Fake) GetVaultEntry(_ context.Context, path string) (*client.VaultEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetVaultEntry:" + path)
	if f.GetVaultEntryErr != nil {
		return nil, f.GetVaultEntryErr
	}
	entry := f.VaultEntries[path]
	if entry == nil {
		return nil, &client.APIError{StatusCode: 404, Message: "entry not found"}
	}
	copy := *entry
	copy.References = slices.Clone(entry.References)
	return &copy, nil
}

// PutVaultValues records the precondition, never the input values.
func (f *Fake) PutVaultValues(_ context.Context, path string, _ map[string]string, version int64) (*client.VaultWriteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("PutVaultValues:" + path)
	f.VaultVersions = append(f.VaultVersions, version)
	if f.PutVaultValuesErr != nil {
		return nil, f.PutVaultValuesErr
	}
	if f.VaultWriteResult != nil {
		result := *f.VaultWriteResult
		result.RefreshRequested = slices.Clone(result.RefreshRequested)
		return &result, nil
	}
	return &client.VaultWriteResult{Path: path, Version: version + 1, RefreshRequested: []string{}}, nil
}

// PurgeVaultEntry records the reviewed version.
func (f *Fake) PurgeVaultEntry(_ context.Context, path string, version int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("PurgeVaultEntry:" + path)
	f.VaultVersions = append(f.VaultVersions, version)
	if f.PurgeVaultEntryErr != nil {
		return f.PurgeVaultEntryErr
	}
	delete(f.VaultEntries, path)
	return nil
}
