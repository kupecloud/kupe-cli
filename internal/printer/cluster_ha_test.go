package printer

import (
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// TestHaDisplay covers every state the compact `cluster list` HA column
// can produce. These strings are user-facing — keep them stable for
// scripts that grep `kupe cluster list` output.
func TestHaDisplay(t *testing.T) {
	tests := []struct {
		name string
		c    *client.Cluster
		want string
	}{
		{name: "nil cluster", c: nil, want: "off"},
		{name: "HA not requested", c: &client.Cluster{HighAvailability: false}, want: "off"},
		{
			name: "HA requested but no status yet",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{Phase: client.PhaseProvisioning},
			},
			want: "pending",
		},
		{
			name: "HA configured but cluster Degraded",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{HAConfigured: true, Phase: client.PhaseDegraded},
			},
			want: "degraded",
		},
		{
			name: "HA configured during Migrating",
			c: &client.Cluster{
				HighAvailability: true,
				// Migrating implies HAConfigured isn't true yet, but be
				// defensive against operator-state edge cases.
				Status: &client.ClusterStatus{HAConfigured: true, Phase: client.PhaseMigrating},
			},
			want: "degraded",
		},
		{
			name: "HA fully healthy",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{HAConfigured: true, Phase: client.PhaseRunning},
			},
			want: "on",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := haDisplay(tt.c); got != tt.want {
				t.Errorf("haDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHaDetailDisplay covers the `cluster get` HA detail line, which
// surfaces the billing-anchor timestamp to tenants.
func TestHaDetailDisplay(t *testing.T) {
	enabledAt := "2026-05-25T14:32:11Z"
	tests := []struct {
		name string
		c    *client.Cluster
		want string
	}{
		{name: "off", c: &client.Cluster{HighAvailability: false}, want: "off"},
		{
			name: "migrating",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{Phase: client.PhaseMigrating},
			},
			want: "migrating (kine→etcd in progress, ~10 min downtime)",
		},
		{
			name: "pending (Provisioning)",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{Phase: client.PhaseProvisioning},
			},
			want: "pending (waiting for 3/3 replicas to be ready)",
		},
		{
			name: "on with timestamp",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{HAConfigured: true, Phase: client.PhaseRunning, HAEnabledAt: enabledAt},
			},
			want: "on — enabled at " + enabledAt,
		},
		{
			name: "degraded with timestamp",
			c: &client.Cluster{
				HighAvailability: true,
				Status:           &client.ClusterStatus{HAConfigured: true, Phase: client.PhaseDegraded, HAEnabledAt: enabledAt},
			},
			want: "degraded — enabled at " + enabledAt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := haDetailDisplay(tt.c); got != tt.want {
				t.Errorf("haDetailDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}
