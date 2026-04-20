package printer

import (
	"time"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// APIKeyColumns are the columns rendered by `kupe apikey list`. IDs are
// UUIDs; the default view truncates them to the first 8 characters plus
// an ellipsis so the table stays readable. Wide mode shows the full ID.
func APIKeyColumns() Columns {
	return Columns{
		{Name: "ID", Get: func(v any) string { return shortID(apikey(v).ID) }},
		{Name: "NAME", Get: func(v any) string { return apikey(v).DisplayName }},
		{Name: "ROLE", Get: func(v any) string { return apikey(v).Role }},
		{Name: "CREATED BY", Get: func(v any) string { return apikey(v).CreatedBy }},
		{Name: "LAST USED", Get: func(v any) string { return agesOrNever(apikey(v).LastUsedAt) }},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(apikey(v).CreatedAt) }},
		{Name: "EXPIRES", Wide: true, Get: func(v any) string { return agesOrNever(apikey(v).ExpiresAt) }},
		{Name: "ID-FULL", Wide: true, Get: func(v any) string { return apikey(v).ID }},
	}
}

// APIKeyDetailColumns is the key:value list used by single-item views.
func APIKeyDetailColumns() Columns {
	return Columns{
		{Name: "ID", Get: func(v any) string { return apikey(v).ID }},
		{Name: "Name", Get: func(v any) string { return apikey(v).DisplayName }},
		{Name: "Role", Get: func(v any) string { return apikey(v).Role }},
		{Name: "Created By", Get: func(v any) string { return apikey(v).CreatedBy }},
		{Name: "Created", Get: func(v any) string { return agesOrNever(apikey(v).CreatedAt) }},
		{Name: "Last Used", Get: func(v any) string { return agesOrNever(apikey(v).LastUsedAt) }},
		{Name: "Expires", Get: func(v any) string { return agesOrNever(apikey(v).ExpiresAt) }},
	}
}

func apikey(v any) *client.APIKey {
	switch x := v.(type) {
	case client.APIKey:
		return &x
	case *client.APIKey:
		return x
	}
	return &client.APIKey{}
}

// shortID returns the first 8 chars of id + "…". For IDs shorter than 8
// chars (unusual but possible in tests), returns the ID unchanged.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}

// agesOrNever returns "never" for empty timestamps; otherwise the relative
// age string ("3d", "2h", "5m"). Used for LastUsedAt / ExpiresAt / CreatedAt
// in detail views.
func agesOrNever(ts string) string {
	if ts == "" {
		return "never"
	}
	if age := ageFromRFC3339(ts); age != "" {
		// For future timestamps (expires-at), ageFromRFC3339 returns "0s"
		// because we clamp at zero. Detect and format ahead.
		if t, err := time.Parse(time.RFC3339, ts); err == nil && t.After(time.Now()) {
			return "in " + shortDuration(time.Until(t))
		}
		return age + " ago"
	}
	return ts
}
