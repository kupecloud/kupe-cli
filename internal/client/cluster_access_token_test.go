package client

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestClusterAccessTokens_PathsAndBodies(t *testing.T) {
	const base = "/api/v1/tenants/acme/clusters/dev/access-tokens"
	const id = "cat-0123456789abcdef"
	days := 30
	admin := ClusterAccessTokenAccess{Role: ClusterAccessTokenRoleAdmin}
	tests := []struct {
		name     string
		method   string
		path     string
		wantBody string
		call     func(c *Client) error
	}{
		{"list", http.MethodGet, base, "", func(c *Client) error {
			_, err := c.ListClusterAccessTokens(context.Background(), "dev")
			return err
		}},
		{"get", http.MethodGet, base + "/" + id, "", func(c *Client) error {
			_, err := c.GetClusterAccessToken(context.Background(), "dev", id)
			return err
		}},
		{"create with the default expiry", http.MethodPost, base, `{"displayName":"ci","access":{"role":"admin"}}`, func(c *Client) error {
			_, err := c.CreateClusterAccessToken(context.Background(), "dev", CreateClusterAccessTokenRequest{DisplayName: "ci", Access: admin})
			return err
		}},
		{"create with an expiry", http.MethodPost, base, `{"displayName":"ci","access":{"role":"admin"},"expiresInDays":30}`, func(c *Client) error {
			_, err := c.CreateClusterAccessToken(context.Background(), "dev", CreateClusterAccessTokenRequest{DisplayName: "ci", Access: admin, ExpiresInDays: &days})
			return err
		}},
		{"revoke", http.MethodDelete, base + "/" + id, "", func(c *Client) error {
			return c.DeleteClusterAccessToken(context.Background(), "dev", id)
		}},
		{"collect sends no body", http.MethodPost, base + "/" + id + "/token", "", func(c *Client) error {
			got, err := c.CollectClusterAccessToken(context.Background(), "dev", id)
			if err == nil && (got.Token != "tok" || got.Kubeconfig != "kc" || got.Endpoint != "https://e" || got.CertificateAuthority != "Y2E=") {
				return fmt.Errorf("collect decoded %+v", got)
			}
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectEscaped(t, r, tt.method, tt.path, "")
				if body := rawBody(t, r); body != tt.wantBody {
					t.Errorf("body = %q; want %q", body, tt.wantBody)
				}
				switch {
				case r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == base:
					fmt.Fprintf(w, `{"items":[{"id":%q,"collected":true,"status":{"phase":"Ready"}}]}`, id)
				case r.URL.Path == base+"/"+id+"/token":
					fmt.Fprintf(w, `{"id":%q,"cluster":"dev","token":"tok","kubeconfig":"kc","endpoint":"https://e","certificateAuthority":"Y2E=","expiresAt":"2027-02-20T10:00:00Z"}`, id)
				default:
					fmt.Fprintf(w, `{"id":%q,"status":{"phase":"Pending"}}`, id)
				}
			})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClusterAccessTokens_CodesAreLowerCased(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
		fmt.Fprint(w, `{"code":"ACCESS_TOKEN_ALREADY_COLLECTED","severity":"error","message":"the access token was already collected"}`)
	})
	_, err := c.CollectClusterAccessToken(context.Background(), "dev", "cat-0123456789abcdef")
	if ErrorCode(err) != ClusterAccessTokenCodeAlreadyCollected || StatusCode(err) != http.StatusGone {
		t.Fatalf("err = %v (code %q)", err, ErrorCode(err))
	}
}
