package printer

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

var updateGolden = flag.Bool("update", false, "Update printer golden files")

func TestSecretDeliveryGoldens(t *testing.T) {
	// Future creation time clamps age to 0s, keeping the golden deterministic.
	s := client.Secret{Name: "payments", SecretPath: "apps/payments", Generation: 4, CreatedAt: "2099-01-01T00:00:00Z",
		Sync: []client.SyncTarget{{Cluster: "eu", Namespace: "api", Keys: []string{"API_KEY", "tls.crt"}}, {Cluster: "us", Namespace: "api", SecretName: "payment-values"}},
		Status: &client.SecretStatus{Phase: "Degraded", ObservedGeneration: 4, ObservedRefresh: "opaque-refresh-token", Refresh: &client.SecretRefreshStatus{RequestedAt: "opaque-refresh-token", ObservedAt: "opaque-observed-token", Pending: true}, SyncStatuses: []client.SecretTargetStatus{
			{Cluster: "eu", Namespace: "api", SecretName: "payments", Phase: "Synced", Reason: "Delivered", DeliveredRefresh: "opaque-delivered-token", LastDeliveredAt: "timestamp-must-not-appear"},
			{Cluster: "us", Namespace: "api", SecretName: "payment-values", Phase: "Failed", Reason: "ProviderError"},
			{Cluster: "old", Namespace: "api", SecretName: "payments", Phase: "Withdrawing", Reason: "Withdrawing"},
		}},
	}
	for _, kind := range []string{"list", "get"} {
		t.Run(kind, func(t *testing.T) {
			var out bytes.Buffer
			format, _ := Parse("table")
			var err error
			if kind == "list" {
				err = RenderList(&out, format, []client.Secret{s}, SecretColumns(false), func(v client.Secret) string { return v.Name })
			} else {
				err = RenderOne(&out, format, &s, SecretDetailColumns(false), func(v *client.Secret) string { return v.Name })
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"opaque-refresh-token", "opaque-observed-token", "opaque-delivered-token", "timestamp-must-not-appear", "2099-01-01T00:00:00Z"} {
				if strings.Contains(out.String(), forbidden) {
					t.Fatalf("table leaked %q", forbidden)
				}
			}
			path := filepath.Join("testdata", "secret-"+kind+".golden")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
					t.Fatal(err)
				}
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
	// Structured output retains the complete response, including opaque tokens.
	for _, kind := range []string{"json", "yaml"} {
		var out bytes.Buffer
		format, _ := Parse(kind)
		if err := RenderOne(&out, format, &s, nil, func(v *client.Secret) string { return v.Name }); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "opaque-delivered-token") {
			t.Fatalf("%s dropped raw status", kind)
		}
	}
}
