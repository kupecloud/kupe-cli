package cli

import (
	"strings"

	"github.com/kupecloud/kupe-cli/internal/config"
)

// apiKeyPrefix is the kupe-api API-key format. A direct token (--token /
// KUPE_API_TOKEN) carrying it is an API key; any other direct token is
// assumed to be an OIDC bearer.
const apiKeyPrefix = "kupe_"

// IsAPIKeyAuth reports whether the resolved context authenticates with an
// API key rather than an OIDC login. Owner-only operations (tenant deletion,
// user erasure) are refused server-side for API keys, so commands check this
// first and explain that an OIDC login is required instead of surfacing a
// bare 403. A direct token decides by prefix; otherwise the context's
// authMethod decides (empty == apikey, for pre-OIDC contexts). No context at
// all counts as API-key auth: there is no OIDC identity to act as.
func IsAPIKeyAuth(r *config.Resolved) bool {
	if r == nil {
		return true
	}
	if r.DirectToken != "" {
		return strings.HasPrefix(r.DirectToken, apiKeyPrefix)
	}
	return r.AuthMethod != config.AuthMethodOIDC
}
