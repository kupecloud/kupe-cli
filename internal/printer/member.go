package printer

import "github.com/kupecloud/kupe-cli/internal/client"

// MemberColumns are the columns for `kupe member list`.
func MemberColumns() Columns {
	return Columns{
		{Name: "EMAIL", Get: func(v any) string { return member(v).Email }},
		{Name: "ROLE", Get: func(v any) string { return member(v).Role }},
	}
}

func member(v any) *client.Member { return rowOf[client.Member](v) }
