package printer

import (
	"time"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// ClusterAccessToken renders cluster access tokens. Never a token: list
// and get responses carry none.
var ClusterAccessToken = Resource[client.ClusterAccessToken]{
	Columns:       clusterAccessTokenColumns,
	DetailColumns: clusterAccessTokenDetailColumns,
	Name:          func(t *client.ClusterAccessToken) string { return t.ID },
}

// statusCollected is the STATUS of a Ready token whose token was handed
// over.
const statusCollected = "Collected"

func clusterAccessTokenColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "ID", Get: func(v any) string { return clusterAccessToken(v).ID }},
		{Name: "NAME", Get: func(v any) string { return clusterAccessToken(v).DisplayName }},
		{Name: "ROLE", Get: func(v any) string { return clusterAccessToken(v).Access.Role }},
		{Name: "CREATED-BY", Get: func(v any) string { return clusterAccessToken(v).CreatedBy }},
		{Name: "EXPIRES", Get: func(v any) string { return dateOf(clusterAccessToken(v).ExpiresAt) }},
		{Name: "STATUS", Get: func(v any) string {
			return ux.ColorPhase(ClusterAccessTokenStatus(clusterAccessToken(v)), colorEnabled)
		}},
		{Name: "CREATED", Wide: true, Get: func(v any) string { return dateOf(clusterAccessToken(v).CreatedAt) }},
	}
}

func clusterAccessTokenDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "ID", Get: func(v any) string { return clusterAccessToken(v).ID }},
		{Name: "Name", Get: func(v any) string { return clusterAccessToken(v).DisplayName }},
		{Name: "Cluster", Get: func(v any) string { return clusterAccessToken(v).Cluster }},
		{Name: "Role", Get: func(v any) string { return clusterAccessToken(v).Access.Role }},
		{Name: "Status", Get: func(v any) string {
			return ux.ColorPhase(ClusterAccessTokenStatus(clusterAccessToken(v)), colorEnabled)
		}},
		{Name: "Message", Get: func(v any) string { return valueOf(clusterAccessToken(v).Status).Message }},
		{Name: "Created By", Get: func(v any) string { return clusterAccessToken(v).CreatedBy }},
		{Name: "Created", Get: func(v any) string { return clusterAccessToken(v).CreatedAt }},
		{Name: "Expires", Get: func(v any) string { return clusterAccessToken(v).ExpiresAt }},
		{Name: "Collected", Get: func(v any) string { return orNo(clusterAccessToken(v).CollectedAt) }},
	}
}

// ClusterAccessTokenStatus is a token's STATUS: Collected once a Ready
// token was handed over, else its phase (Pending when the platform has not
// set one yet).
func ClusterAccessTokenStatus(t *client.ClusterAccessToken) string {
	phase := t.Phase()
	switch {
	case phase == "":
		return client.PhasePending
	case phase == client.PhaseReady && t.Collected:
		return statusCollected
	}
	return phase
}

// dateOf is an RFC 3339 timestamp's UTC date ("2027-02-20"), or the value
// as given when it does not parse.
func dateOf(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format(time.DateOnly)
}

// orNo is s, or "no" when it is empty.
func orNo(s string) string {
	if s == "" {
		return "no"
	}
	return s
}

func clusterAccessToken(v any) *client.ClusterAccessToken {
	return rowOf[client.ClusterAccessToken](v)
}
