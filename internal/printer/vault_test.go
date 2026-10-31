package printer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestVaultMetadataDetailColumns(t *testing.T) {
	var out bytes.Buffer
	entry := &client.VaultEntry{Path: "team/db", Version: 9, UpdatedAt: "2026-10-29T12:00:00Z", Deleted: true, References: []string{"a", "b"}}
	if err := PrintDetails(&out, entry, VaultEntryColumns()); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Path:", "team/db", "Version:", "9", "Updated:", entry.UpdatedAt, "Deleted:", "true", "References:", "a, b"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %s", text, out.String())
		}
	}
}
