package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

const testDigest = "sha256:4f8c0d0e4b1f2b6e7e0c6a3d6c0f3a9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a"

// registryJSON is a representative GET /registries/registry body.
const registryJSON = `{"name":"registry","storageLimit":"10Gi","deletionProtection":true,
  "status":{"observedGeneration":2,"phase":"Running","endpoint":"registry.kupe.cloud/acme",
    "storageUsedBytes":1288490188,"storageLimitBytes":10737418240,"repositoryCount":3,"artifactCount":12,
    "usageObservedAt":"2026-10-22T10:00:00Z","conditions":[{"type":"Ready","status":"True"}]},
  "generation":2,"resourceVersion":"582019","createdAt":"2026-10-21T10:00:00Z"}`

// expectEscaped fails the test unless r has the given method and escaped
// path (so a %2F the client sent is checked as sent) and raw query.
func expectEscaped(t *testing.T, r *http.Request, method, path, query string) {
	t.Helper()
	if r.Method != method || r.URL.EscapedPath() != path || r.URL.RawQuery != query {
		t.Errorf("request = %s %s?%s; want %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery, method, path, query)
	}
}

// rawBody returns the request body as sent.
func rawBody(t *testing.T, r *http.Request) string {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(raw)
}

func TestRegistries_CRUDPaths(t *testing.T) {
	const base = "/api/v1/tenants/acme/registries"
	off := false
	limit := ""
	tests := []struct {
		name     string
		method   string
		path     string
		wantBody string // "" = no body checked
		call     func(c *Client) error
	}{
		{"list", http.MethodGet, base, "", func(c *Client) error { _, err := c.Registries().List(context.Background()); return err }},
		{"get", http.MethodGet, base + "/registry", "", func(c *Client) error {
			_, _, err := c.Registries().Get(context.Background(), RegistryName)
			return err
		}},
		{"create takes the default protection", http.MethodPost, base, `{"name":"registry"}`, func(c *Client) error {
			_, _, err := c.Registries().Create(context.Background(), CreateRegistryRequest{Name: RegistryName})
			return err
		}},
		{"create without protection", http.MethodPost, base, `{"name":"registry","storageLimit":"5Gi","deletionProtection":false}`, func(c *Client) error {
			_, _, err := c.Registries().Create(context.Background(), CreateRegistryRequest{Name: RegistryName, StorageLimit: "5Gi", DeletionProtection: &off})
			return err
		}},
		{"patch clears the limit", http.MethodPatch, base + "/registry", `{"storageLimit":""}`, func(c *Client) error {
			_, _, err := c.Registries().Update(context.Background(), RegistryName, "", PatchRegistryRequest{StorageLimit: &limit})
			return err
		}},
		{"delete", http.MethodDelete, base + "/registry", "", func(c *Client) error {
			return c.Registries().Delete(context.Background(), RegistryName)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectEscaped(t, r, tt.method, tt.path, "")
				if tt.wantBody != "" {
					if got := rawBody(t, r); got != tt.wantBody {
						t.Errorf("body = %s; want %s", got, tt.wantBody)
					}
				}
				switch r.Method {
				case http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				case http.MethodGet:
					if r.URL.Path == base {
						fmt.Fprintf(w, `{"items":[%s]}`, registryJSON)
						return
					}
					fmt.Fprintln(w, registryJSON)
				default:
					fmt.Fprintln(w, registryJSON)
				}
			})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGetRegistry_DecodesStatus(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", "582019")
		fmt.Fprintln(w, registryJSON)
	})
	reg, etag, err := c.Registries().Get(context.Background(), RegistryName)
	if err != nil {
		t.Fatal(err)
	}
	st := reg.Status
	if etag != "582019" || !reg.DeletionProtection || reg.StorageLimit != "10Gi" || st.Phase != PhaseRunning ||
		st.StorageUsedBytes != 1288490188 || st.StorageLimitBytes == nil || *st.StorageLimitBytes != 10737418240 ||
		st.RepositoryCount != 3 || st.Host() != "registry.kupe.cloud" {
		t.Fatalf("decoded registry = %+v status %+v", reg, st)
	}
}

func TestRegistryStatus_Host(t *testing.T) {
	var none *RegistryStatus
	if none.Host() != "" || (&RegistryStatus{}).Host() != "" || (&RegistryStatus{Endpoint: "registry.kupe.cloud/acme"}).Host() != "registry.kupe.cloud" {
		t.Fatal("Host() wrong")
	}
}

func TestRegistryBindings_Paths(t *testing.T) {
	const base = "/api/v1/tenants/acme/registries/registry/bindings"
	cleared := []string{}
	tests := []struct {
		name     string
		method   string
		path     string
		wantBody string
		call     func(c *Client) error
	}{
		{"list", http.MethodGet, base, "", func(c *Client) error {
			_, err := c.RegistryBindings(RegistryName).List(context.Background())
			return err
		}},
		{"get", http.MethodGet, base + "/prod", "", func(c *Client) error {
			_, _, err := c.RegistryBindings(RegistryName).Get(context.Background(), "prod")
			return err
		}},
		{"create pull only", http.MethodPost, base, `{"name":"prod","cluster":"prod"}`, func(c *Client) error {
			_, _, err := c.RegistryBindings(RegistryName).Create(context.Background(), CreateRegistryBindingRequest{Name: "prod", Cluster: "prod"})
			return err
		}},
		{"create with push", http.MethodPost, base, `{"name":"prod","cluster":"prod","pushNamespaces":["ci"],"secretName":"push"}`, func(c *Client) error {
			_, _, err := c.RegistryBindings(RegistryName).Create(context.Background(),
				CreateRegistryBindingRequest{Name: "prod", Cluster: "prod", PushNamespaces: []string{"ci"}, SecretName: "push"})
			return err
		}},
		{"patch to no push namespaces sends []", http.MethodPatch, base + "/prod", `{"pushNamespaces":[]}`, func(c *Client) error {
			_, _, err := c.RegistryBindings(RegistryName).Update(context.Background(), "prod", "", PatchRegistryBindingRequest{PushNamespaces: &cleared})
			return err
		}},
		{"delete", http.MethodDelete, base + "/prod", "", func(c *Client) error {
			return c.RegistryBindings(RegistryName).Delete(context.Background(), "prod")
		}},
		{"rotate", http.MethodPost, base + "/prod/rotate", "", func(c *Client) error {
			rot, err := c.RotateRegistryBinding(context.Background(), RegistryName, "prod")
			if err == nil && (rot.Binding != "prod" || rot.RequestedAt != "2026-10-22T12:00:00.1Z") {
				err = fmt.Errorf("rotation = %+v", rot)
			}
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectEscaped(t, r, tt.method, tt.path, "")
				body := rawBody(t, r)
				if tt.wantBody != "" && body != tt.wantBody {
					t.Errorf("body = %s; want %s", body, tt.wantBody)
				}
				if tt.name == "rotate" && body != "" {
					t.Errorf("rotate sent a body: %s", body)
				}
				switch {
				case r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				case tt.name == "rotate":
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprint(w, `{"binding":"prod","requestedAt":"2026-10-22T12:00:00.1Z"}`)
				case r.URL.Path == base && r.Method == http.MethodGet:
					fmt.Fprint(w, `{"items":[]}`)
				default:
					fmt.Fprint(w, `{"name":"prod","cluster":"prod"}`)
				}
			})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRegistryBinding_DecodesStatus(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"name":"prod","registry":"registry","cluster":"prod","pushNamespaces":["ci"],"secretName":"kupe-registry-push",
		  "status":{"phase":"Ready","pullUsername":"kupe+acme+pull-prod","pushUsername":"kupe+acme+push-prod",
		    "namespaces":[{"name":"ci","phase":"Ready"}],"rotation":{"requestedAt":"a","appliedAt":"a","pending":false},
		    "conditions":[{"type":"Conflict","status":"True"}]}}`)
	})
	b, _, err := c.RegistryBindings(RegistryName).Get(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if b.Status.PullUsername != "kupe+acme+pull-prod" || b.Status.Namespaces[0].Name != "ci" || b.Status.Rotation.AppliedAt != "a" ||
		!b.Status.Conflicted() || b.PushNamespaces[0] != "ci" {
		t.Fatalf("decoded binding = %+v", b)
	}
}

func TestRepositoriesAndImages_PathsAndQueries(t *testing.T) {
	const base = "/api/v1/tenants/acme/registries/registry/repositories"
	tagged := false
	tests := []struct {
		name   string
		method string
		path   string
		query  string
		call   func(c *Client) error
	}{
		{"list repositories, defaults", http.MethodGet, base, "", func(c *Client) error {
			_, err := c.ListRepositories(context.Background(), RegistryName, ListRepositoriesOptions{})
			return err
		}},
		{"list repositories, search and page", http.MethodGet, base, "page=2&pageSize=100&search=team-a%2F", func(c *Client) error {
			_, err := c.ListRepositories(context.Background(), RegistryName, ListRepositoriesOptions{Search: "team-a/", Page: 2, PageSize: 100})
			return err
		}},
		{"get repository keeps the slash escaped", http.MethodGet, base + "/team-a%2Fapi", "", func(c *Client) error {
			_, err := c.GetRepository(context.Background(), RegistryName, "team-a/api")
			return err
		}},
		{"delete repository", http.MethodDelete, base + "/team-a%2Fapi", "", func(c *Client) error {
			return c.DeleteRepository(context.Background(), RegistryName, "team-a/api")
		}},
		{"list images untagged", http.MethodGet, base + "/team-a%2Fapi/images", "page=1&tagged=false", func(c *Client) error {
			_, err := c.ListImages(context.Background(), RegistryName, "team-a/api", ListImagesOptions{Tagged: &tagged, Page: 1})
			return err
		}},
		{"get image by digest keeps the colon", http.MethodGet, base + "/team-a%2Fapi/images/" + testDigest, "", func(c *Client) error {
			_, err := c.GetImage(context.Background(), RegistryName, "team-a/api", testDigest)
			return err
		}},
		{"get image by tag", http.MethodGet, base + "/api/images/v1.4.2", "", func(c *Client) error {
			_, err := c.GetImage(context.Background(), RegistryName, "api", "v1.4.2")
			return err
		}},
		{"delete image", http.MethodDelete, base + "/team-a%2Fapi/images/" + testDigest, "", func(c *Client) error {
			return c.DeleteImage(context.Background(), RegistryName, "team-a/api", testDigest)
		}},
		{"delete tag", http.MethodDelete, base + "/team-a%2Fapi/tags/v1.4.2", "", func(c *Client) error {
			return c.DeleteTag(context.Background(), RegistryName, "team-a/api", "v1.4.2")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectEscaped(t, r, tt.method, tt.path, tt.query)
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				fmt.Fprint(w, `{"items":[],"page":1,"pageSize":25,"total":0}`)
			})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListImages_DecodesPage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"items":[{"repository":"team-a/api","digest":%q,"type":"index","size":12345678,
		  "tags":[{"name":"v1.4.2"}],"platforms":[{"os":"linux","architecture":"arm64","variant":"v8"}],
		  "pull":{"byDigest":"registry.kupe.cloud/acme/team-a/api@x","byTag":["registry.kupe.cloud/acme/team-a/api:v1.4.2"]},
		  "links":{"self":"/api/v1/x"}}],"page":2,"pageSize":1,"total":42}`, testDigest)
	})
	page, err := c.ListImages(context.Background(), RegistryName, "team-a/api", ListImagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != 2 || page.PageSize != 1 || page.Total != 42 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	img := page.Items[0]
	if img.Digest != testDigest || img.Tags[0].Name != "v1.4.2" || img.Platforms[0].Variant != "v8" || img.Pull.ByTag[0] == "" || img.Size != 12345678 {
		t.Fatalf("image = %+v", img)
	}
}

func TestRegistryTokens_PathsAndBodies(t *testing.T) {
	const base = "/api/v1/tenants/acme/registries/registry"
	days := 30
	tests := []struct {
		name     string
		method   string
		path     string
		query    string
		wantBody string
		call     func(c *Client) error
	}{
		{"list both kinds", http.MethodGet, base + "/tokens", "", "", func(c *Client) error {
			_, err := c.ListRegistryTokens(context.Background(), RegistryName, "")
			return err
		}},
		{"list sessions", http.MethodGet, base + "/tokens", "kind=session", "", func(c *Client) error {
			_, err := c.ListRegistryTokens(context.Background(), RegistryName, RegistryTokenKindSession)
			return err
		}},
		{"get", http.MethodGet, base + "/tokens/rt-0123456789abcdef", "", "", func(c *Client) error {
			_, err := c.GetRegistryToken(context.Background(), RegistryName, "rt-0123456789abcdef")
			return err
		}},
		{"create with the default expiry", http.MethodPost, base + "/tokens", "", `{"displayName":"ci","access":"pull"}`, func(c *Client) error {
			_, err := c.CreateRegistryToken(context.Background(), RegistryName, CreateRegistryTokenRequest{DisplayName: "ci", Access: RegistryAccessPull})
			return err
		}},
		{"create with an expiry", http.MethodPost, base + "/tokens", "", `{"displayName":"ci","access":"push","expiresInDays":30}`, func(c *Client) error {
			_, err := c.CreateRegistryToken(context.Background(), RegistryName, CreateRegistryTokenRequest{DisplayName: "ci", Access: RegistryAccessPush, ExpiresInDays: &days})
			return err
		}},
		{"revoke", http.MethodDelete, base + "/tokens/rt-0123456789abcdef", "", "", func(c *Client) error {
			return c.DeleteRegistryToken(context.Background(), RegistryName, "rt-0123456789abcdef")
		}},
		{"session sends no body", http.MethodPost, base + "/sessions", "", "", func(c *Client) error {
			_, err := c.CreateRegistrySession(context.Background(), RegistryName)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectEscaped(t, r, tt.method, tt.path, tt.query)
				body := rawBody(t, r)
				if body != tt.wantBody {
					t.Errorf("body = %q; want %q", body, tt.wantBody)
				}
				switch {
				case r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == base+"/tokens":
					fmt.Fprint(w, `{"items":[]}`)
				default:
					fmt.Fprint(w, `{"id":"rt-0123456789abcdef"}`)
				}
			})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCreateRegistrySession_DecodesSecretAndHost(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"rt-0123456789abcdef","displayName":"kupe registry login","kind":"session","access":"push",
		  "createdBy":"a@acme.com","createdAt":"2026-10-22T10:00:00Z","expiresAt":"2026-10-22T22:00:00Z",
		  "registry":"registry.kupe.cloud","username":"kupe+acme+cli-0123456789abcdef","secret":"kupecr_x","status":{"phase":""}}`)
	})
	s, err := c.CreateRegistrySession(context.Background(), RegistryName)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "rt-0123456789abcdef" || s.Kind != RegistryTokenKindSession || s.Registry != "registry.kupe.cloud" ||
		s.Secret != "kupecr_x" || s.Username != "kupe+acme+cli-0123456789abcdef" || s.ExpiresAt == "" || s.Phase() != "" {
		t.Fatalf("session = %+v", s)
	}
	// The embedded token's fields stay flat in JSON, as the API sends them.
	out, _ := json.Marshal(s)
	var flat map[string]any
	_ = json.Unmarshal(out, &flat)
	if flat["id"] != "rt-0123456789abcdef" || flat["secret"] != "kupecr_x" {
		t.Fatalf("re-encoded = %s", out)
	}
}

func TestRegistryErrors_CodesAndMissing(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantCode    string
		wantMissing bool
	}{
		{"webhook code lower-cased", 409, `{"code":"REGISTRY_ALREADY_EXISTS","message":"a tenant has one registry"}`, RegistryCodeAlreadyExists, false},
		{"switched off", 404, `{"code":"registries_unavailable","message":"registries are not available on this platform"}`, RegistryCodeUnavailable, false},
		{"no registry", 404, `{"error":"registry \"registry\" not found"}`, "", true},
		{"repository missing is not the registry", 404, `{"error":"repository \"api\" not found"}`, "", false},
		{"connection missing is not the registry", 404, `{"error":"cluster connection \"prod\" not found"}`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})
			_, err := c.GetRepository(context.Background(), RegistryName, "api")
			if err == nil {
				t.Fatal("want error")
			}
			if got := ErrorCode(err); got != tt.wantCode {
				t.Errorf("ErrorCode = %q; want %q", got, tt.wantCode)
			}
			if got := IsRegistryMissing(err); got != tt.wantMissing {
				t.Errorf("IsRegistryMissing = %v; want %v", got, tt.wantMissing)
			}
		})
	}
}
