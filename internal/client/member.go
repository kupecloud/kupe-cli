package client

import (
	"context"
	"net/http"
)

// Member is a tenant member — a user identified by email with a tenant
// role (admin or readonly). Members sync to Authentik groups via the
// kupe-control-operator.
type Member struct {
	Email string `json:"email" yaml:"email"`
	Role  string `json:"role" yaml:"role"`
}

// AddMemberRequest is the POST body.
type AddMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// UpdateMemberRequest updates only the role.
type UpdateMemberRequest struct {
	Role string `json:"role"`
}

// ListMembers returns every member of the tenant.
func (c *Client) ListMembers(ctx context.Context) ([]Member, error) {
	var resp struct {
		Items []Member `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("members"), nil, &resp)
	return resp.Items, err
}

// AddMember adds a member to the tenant. Duplicate emails yield 409.
func (c *Client) AddMember(ctx context.Context, req AddMemberRequest) (*Member, error) {
	var member Member
	_, err := c.request(ctx, http.MethodPost, c.tenantPath("members"), req, &member)
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// UpdateMember changes a member's role.
func (c *Client) UpdateMember(ctx context.Context, email string, req UpdateMemberRequest) (*Member, error) {
	var member Member
	_, err := c.request(ctx, http.MethodPatch, c.tenantPath("members", email), req, &member)
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// RemoveMember removes a member from the tenant.
func (c *Client) RemoveMember(ctx context.Context, email string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("members", email), nil, nil)
	return err
}
