package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// SignupInterface is the surface `kupe user` commands depend on. The signup
// service (not kupe-api) owns self-service user operations; it is a separate
// base URL (config.DefaultSignupURL) and only accepts OIDC bearer tokens.
type SignupInterface interface {
	// DeleteUser calls DELETE /users/me with a typed-email confirmation.
	// nil on 204 (idempotent server-side).
	DeleteUser(ctx context.Context, req DeleteUserRequest) error
}

// DeleteUserRequest is the DELETE /users/me body. Confirm must equal the
// email in the caller's OIDC token (400 otherwise).
type DeleteUserRequest struct {
	Confirm string `json:"confirm"`
}

// UserDeleteCodeTenantMemberships is the 409 code the signup service returns
// when the user is still a member of one or more tenants. The response lists
// the tenant names; see TenantMembershipsError.
const UserDeleteCodeTenantMemberships = "tenant_memberships"

// TenantMembershipsError is the typed form of signup's 409
// tenant_memberships response: the user must delete (as owner) or leave
// every listed tenant before their account can be erased. Unwraps to the
// underlying *APIError so IsConflict / ExitCode keep working.
type TenantMembershipsError struct {
	Err     *APIError
	Tenants []string
}

func (e *TenantMembershipsError) Error() string {
	if len(e.Tenants) == 0 {
		return e.Err.Error()
	}
	return fmt.Sprintf("still a member of tenant(s): %s", strings.Join(e.Tenants, ", "))
}

// Unwrap exposes the APIError for errors.As / the Is* classifiers.
func (e *TenantMembershipsError) Unwrap() error { return e.Err }

// SignupClient talks to the signup service. It reuses the kupe-api client's
// transport (retry, 429 handling, typed errors, per-request token source)
// with a different base URL and no tenant scope.
type SignupClient struct {
	c *Client
}

// NewSignup builds a SignupClient for baseURL. token may be "" when a
// WithTokenSource option supplies it per request.
func NewSignup(baseURL, token, userAgent string, opts ...Option) *SignupClient {
	return &SignupClient{c: New(baseURL, "", token, userAgent, opts...)}
}

// BaseURL returns the configured signup service base URL.
func (s *SignupClient) BaseURL() string { return s.c.BaseURL() }

// DeleteUser implements SignupInterface. A 409 tenant_memberships response
// is returned as *TenantMembershipsError; every other non-2xx is the usual
// *APIError (400 bad confirm, 403 not OIDC / stale auth / refused identity,
// 429 rate limited).
func (s *SignupClient) DeleteUser(ctx context.Context, req DeleteUserRequest) error {
	_, err := s.c.request(ctx, http.MethodDelete, "/users/me", req, nil)
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return &TenantMembershipsError{Err: apiErr, Tenants: parseMembershipTenants(apiErr.Body())}
	}
	return err
}

// parseMembershipTenants pulls the tenant names out of a 409 body. The
// contract says the response "lists tenant names"; accept the plausible
// shapes — `tenants: ["a","b"]`, `tenants: [{"name":"a"}]`, or the same
// under `details` — and return nil when none is present so the caller falls
// back to the server's message.
func parseMembershipTenants(body []byte) []string {
	if len(body) == 0 {
		return nil
	}
	var env struct {
		Tenants json.RawMessage `json:"tenants"`
		Details struct {
			Tenants json.RawMessage `json:"tenants"`
		} `json:"details"`
	}
	if json.Unmarshal(body, &env) != nil {
		return nil
	}
	raw := env.Tenants
	if len(raw) == 0 {
		raw = env.Details.Tenants
	}
	if len(raw) == 0 {
		return nil
	}
	var names []string
	if json.Unmarshal(raw, &names) == nil {
		return names
	}
	names = nil // a partial decode of an object list leaves zero-value entries behind
	var objs []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &objs) == nil {
		for _, o := range objs {
			if o.Name != "" {
				names = append(names, o.Name)
			}
		}
	}
	return names
}

// Compile-time interface check.
var _ SignupInterface = (*SignupClient)(nil)
