package printer

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// VaultEntryColumns renders only the metadata returned by a Vault read.
func VaultEntryColumns() Columns {
	entry := func(v any) *client.VaultEntry { return rowOf[client.VaultEntry](v) }
	return Columns{
		{Name: "Path", Get: func(v any) string { return entry(v).Path }},
		{Name: "Version", Get: func(v any) string { return fmt.Sprint(entry(v).Version) }},
		{Name: "Updated", Get: func(v any) string { return entry(v).UpdatedAt }},
		{Name: "Deleted", Get: func(v any) string { return fmt.Sprint(entry(v).Deleted) }},
		{Name: "References", Get: func(v any) string { return strings.Join(entry(v).References, ", ") }},
	}
}
