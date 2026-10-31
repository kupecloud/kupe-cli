package clienttest

import (
	"context"
	"errors"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestVaultFakeCopiesResultsAndRecordsVersions(t *testing.T) {
	ctx := context.Background()
	f := New()
	f.VaultEntries = map[string]*client.VaultEntry{"db": {Path: "db", Version: 2, References: []string{"orders"}}}
	entry, err := f.GetVaultEntry(ctx, "db")
	if err != nil {
		t.Fatal(err)
	}
	entry.References[0] = "changed"
	if f.VaultEntries["db"].References[0] != "orders" {
		t.Fatal("fake result aliased")
	}
	f.VaultWriteResult = &client.VaultWriteResult{Path: "db", Version: 3, RefreshRequested: []string{"orders"}}
	result, err := f.PutVaultValues(ctx, "db", map[string]string{"FIELD_A": "synthetic"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	result.RefreshRequested[0] = "changed"
	if f.VaultWriteResult.RefreshRequested[0] != "orders" {
		t.Fatal("ack aliased")
	}
	if err := f.PurgeVaultEntry(ctx, "db", 3); err != nil {
		t.Fatal(err)
	}
	if len(f.VaultEntries) != 0 || len(f.VaultVersions) != 2 || f.VaultVersions[0] != 2 || f.VaultVersions[1] != 3 {
		t.Fatal("state/version not recorded")
	}
	refusal := errors.New("synthetic refusal")
	f.GetVaultEntryErr = refusal
	f.PutVaultValuesErr = refusal
	f.PurgeVaultEntryErr = refusal
	if _, err := f.GetVaultEntry(ctx, "db"); !errors.Is(err, refusal) {
		t.Fatal(err)
	}
	if _, err := f.PutVaultValues(ctx, "db", nil, 3); !errors.Is(err, refusal) {
		t.Fatal(err)
	}
	if err := f.PurgeVaultEntry(ctx, "db", 3); !errors.Is(err, refusal) {
		t.Fatal(err)
	}
}
