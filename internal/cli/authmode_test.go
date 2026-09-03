package cli

import (
	"testing"

	"github.com/kupecloud/kupe-cli/internal/config"
)

func TestIsAPIKeyAuth(t *testing.T) {
	tests := []struct {
		name string
		r    *config.Resolved
		want bool
	}{
		{"nil", nil, true},
		{"no context", &config.Resolved{}, true},
		{"apikey context", &config.Resolved{AuthMethod: config.AuthMethodAPIKey}, true},
		{"oidc context", &config.Resolved{AuthMethod: config.AuthMethodOIDC}, false},
		{"direct api key beats oidc context", &config.Resolved{AuthMethod: config.AuthMethodOIDC, DirectToken: "kupe_abc"}, true},
		{"direct jwt", &config.Resolved{DirectToken: "eyJhbGciOi.eyJzdWIiOi.sig"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAPIKeyAuth(tt.r); got != tt.want {
				t.Fatalf("IsAPIKeyAuth = %v; want %v", got, tt.want)
			}
		})
	}
}
