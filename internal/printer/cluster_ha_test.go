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
			name: "HA configured during Upgrading",
			c: &client.Cluster{
				HighAvailability: true,
				// Upgrading implies a transient non-Running phase; the
				// fallback path treats this as degraded.
				Status: &client.ClusterStatus{HAConfigured: true, Phase: client.PhaseUpgrading},
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

// TestHaDisplay_HAPhaseDriven covers the haPhase-driven path that takes
// precedence when the operator populates the rollup. Includes the new
// ha-unavailable state (quorum lost) which the legacy fallback can't
// distinguish from "degraded".
func TestHaDisplay_HAPhaseDriven(t *testing.T) {
	tests := []struct {
		name    string
		phase   string
		ready   int32
		desired int32
		want    string
	}{
		{name: "ha-healthy with counts", phase: "ha-healthy", ready: 3, desired: 3, want: "on (3/3)"},
		{name: "ha-degraded with counts", phase: "ha-degraded", ready: 2, desired: 3, want: "degraded (2/3)"},
		{name: "ha-unavailable with counts", phase: "ha-unavailable", ready: 0, desired: 3, want: "unavailable (0/3)"},
		{name: "pending", phase: "pending", want: "pending"},
		{name: "ha-healthy without counts (older op)", phase: "ha-healthy", want: "on "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client.Cluster{
				HighAvailability: true,
				Status: &client.ClusterStatus{
					HAPhase:           tt.phase,
					HAReplicasReady:   tt.ready,
					HAReplicasDesired: tt.desired,
				},
			}
			if got := haDisplay(c); got != tt.want {
				t.Errorf("haDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHaDetailDisplay_HAPhaseDriven covers the expanded `cluster get`
// detail line driven by the haPhase rollup, including the new
// ha-unavailable copy that distinguishes "quorum lost" from "redundancy
// reduced" so tenants see the right severity.
func TestHaDetailDisplay_HAPhaseDriven(t *testing.T) {
	enabledAt := "2026-05-25T14:32:11Z"
	tests := []struct {
		name    string
		phase   string
		ready   int32
		desired int32
		want    string
	}{
		{
			name: "ha-healthy", phase: "ha-healthy", ready: 3, desired: 3,
			want: "on (3/3) — enabled at " + enabledAt,
		},
		{
			name: "ha-degraded", phase: "ha-degraded", ready: 2, desired: 3,
			want: "degraded (2/3) — API still serving, enabled at " + enabledAt,
		},
		{
			name: "ha-unavailable", phase: "ha-unavailable", ready: 0, desired: 3,
			want: "unavailable (0/3) — quorum lost, API not serving, enabled at " + enabledAt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client.Cluster{
				HighAvailability: true,
				Status: &client.ClusterStatus{
					HAPhase: tt.phase, HAReplicasReady: tt.ready, HAReplicasDesired: tt.desired,
					HAEnabledAt: enabledAt,
				},
			}
			if got := haDetailDisplay(c); got != tt.want {
				t.Errorf("haDetailDisplay() = %q, want %q", got, tt.want)
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
