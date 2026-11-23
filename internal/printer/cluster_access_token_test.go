package printer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func clusterAccessTokenFixtures() []client.ClusterAccessToken {
	admin := client.ClusterAccessTokenAccess{Role: client.ClusterAccessTokenRoleAdmin}
	return []client.ClusterAccessToken{
		{ID: "cat-0a1b2c3d4e5f6a7b", Cluster: "dev", DisplayName: "terraform", Access: admin, CreatedBy: "alice@acme.com",
			CreatedAt: "2026-11-22T10:00:00Z", ExpiresAt: "2027-02-20T10:00:00Z", CollectedAt: "2026-11-22T10:00:05Z", Collected: true,
			Status: &client.ClusterAccessTokenStatus{Phase: client.PhaseReady}},
		{ID: "cat-1111111111111111", Cluster: "dev", DisplayName: "ci", Access: admin, CreatedBy: "ak-0123456789abcdef",
			CreatedAt: "2026-11-21T09:00:00Z", ExpiresAt: "2026-12-21T09:00:00Z",
			Status: &client.ClusterAccessTokenStatus{Phase: client.PhaseReady}},
		{ID: "cat-2222222222222222", Cluster: "dev", DisplayName: "new", Access: admin, CreatedBy: "alice@acme.com",
			CreatedAt: "2026-11-20T09:00:00Z", ExpiresAt: "2027-11-20T09:00:00Z"},
		{ID: "cat-3333333333333333", Cluster: "dev", DisplayName: "broken", Access: admin, CreatedBy: "alice@acme.com",
			CreatedAt: "2026-11-19T09:00:00Z", ExpiresAt: "2027-02-17T09:00:00Z",
			Status: &client.ClusterAccessTokenStatus{Phase: client.PhaseFailed, Message: "the cluster could not issue the token"}},
		{ID: "cat-4444444444444444", Cluster: "dev", DisplayName: "old", Access: admin, CreatedBy: "alice@acme.com",
			CreatedAt: "2026-11-18T09:00:00Z", ExpiresAt: "2027-02-16T09:00:00Z", Collected: true,
			Status: &client.ClusterAccessTokenStatus{Phase: client.PhaseRevoking}},
	}
}

// TestClusterAccessTokenGoldens pins the list, wide and get tables. None
// has a token column; `make test-update` refreshes the goldens.
func TestClusterAccessTokenGoldens(t *testing.T) {
	tokens := clusterAccessTokenFixtures()
	for _, tt := range []struct{ name, format string }{
		{"list", "table"}, {"list-wide", "wide"}, {"get", "table"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			format, err := Parse(tt.format)
			if err != nil {
				t.Fatal(err)
			}
			name := func(v *client.ClusterAccessToken) string { return v.ID }
			if tt.name == "get" {
				err = RenderOne(&out, format, &tokens[3], clusterAccessTokenDetailColumns(false), name)
			} else {
				err = RenderList(&out, format, tokens, clusterAccessTokenColumns(false), func(v client.ClusterAccessToken) string { return v.ID })
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(out.String()), "token:") {
				t.Fatalf("a token field was rendered:\n%s", out.String())
			}
			path := filepath.Join("testdata", "cluster-access-token-"+tt.name+".golden")
			if *updateGolden {
				if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != string(want) {
				t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
			}
		})
	}
}

func TestClusterAccessTokenStatus(t *testing.T) {
	for _, tt := range []struct {
		phase     string
		collected bool
		want      string
	}{
		{"", false, "Pending"},
		{"Pending", false, "Pending"},
		{"Ready", false, "Ready"},
		{"Ready", true, "Collected"},
		{"Failed", false, "Failed"},
		{"Revoking", true, "Revoking"},
	} {
		tok := &client.ClusterAccessToken{Collected: tt.collected}
		if tt.phase != "" {
			tok.Status = &client.ClusterAccessTokenStatus{Phase: tt.phase}
		}
		if got := ClusterAccessTokenStatus(tok); got != tt.want {
			t.Errorf("phase %q collected %v: status = %q; want %q", tt.phase, tt.collected, got, tt.want)
		}
	}
}
