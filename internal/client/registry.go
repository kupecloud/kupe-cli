package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RegistryName is the name of a tenant's registry. A tenant has at most
// one and it is always called this: kupe-api answers 404 for any other name,
// so no command takes a registry name.
const RegistryName = "registry"

// DefaultRegistryHost is the registry host kupe-api falls back to when the
// platform publishes none. The host a session or token is for comes from
// kupe-api (CreatedRegistryToken.Registry); this is only for commands that
// have nothing to read it from.
const DefaultRegistryHost = "registry.kupe.cloud"

// Registry is the API response shape for the tenant's container registry.
// It never carries a credential.
type Registry struct {
	Name string `json:"name" yaml:"name"`
	// StorageLimit is the tenant's own cap below the plan's ("10Gi"); empty
	// means the plan's cap applies.
	StorageLimit       string          `json:"storageLimit,omitempty" yaml:"storageLimit,omitempty"`
	DeletionProtection bool            `json:"deletionProtection" yaml:"deletionProtection"`
	Status             *RegistryStatus `json:"status,omitempty" yaml:"status,omitempty"`
	// Generation bumps on every spec change; compare with
	// status.observedGeneration to know when a change has been applied.
	Generation      int64  `json:"generation,omitempty" yaml:"generation,omitempty"`
	ResourceVersion string `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string `json:"createdAt" yaml:"createdAt"`
}

// RegistryStatus is the observed state. Phase: Pending | Provisioning |
// Running | Degraded | Terminating.
type RegistryStatus struct {
	ObservedGeneration int64  `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Phase              string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// Endpoint is the image path prefix to push to and pull from
	// ("registry.kupe.cloud/acme").
	Endpoint         string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	StorageUsedBytes int64  `json:"storageUsedBytes" yaml:"storageUsedBytes"`
	// StorageLimitBytes is the cap in force: -1 means unlimited, nil means
	// no cap has been applied yet.
	StorageLimitBytes *int64      `json:"storageLimitBytes,omitempty" yaml:"storageLimitBytes,omitempty"`
	RepositoryCount   int64       `json:"repositoryCount" yaml:"repositoryCount"`
	ArtifactCount     int64       `json:"artifactCount" yaml:"artifactCount"`
	UsageObservedAt   string      `json:"usageObservedAt,omitempty" yaml:"usageObservedAt,omitempty"`
	Conditions        []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// Host is the registry host of the endpoint ("registry.kupe.cloud" of
// "registry.kupe.cloud/acme"), "" while the platform reports none.
func (s *RegistryStatus) Host() string {
	if s == nil {
		return ""
	}
	host, _, _ := strings.Cut(s.Endpoint, "/")
	return host
}

// CreateRegistryRequest is the POST body. An empty StorageLimit takes the
// plan's cap; a nil DeletionProtection takes the default (on).
type CreateRegistryRequest struct {
	Name               string `json:"name"`
	StorageLimit       string `json:"storageLimit,omitempty"`
	DeletionProtection *bool  `json:"deletionProtection,omitempty"`
}

// PatchRegistryRequest is the PATCH (JSON merge-patch) body. Nil fields are
// left unchanged; a StorageLimit of "" removes the limit.
type PatchRegistryRequest struct {
	StorageLimit       *string `json:"storageLimit,omitempty"`
	DeletionProtection *bool   `json:"deletionProtection,omitempty"`
}

// RegistryBinding is a cluster connection: every pod of the cluster can pull
// from the registry, and each push namespace holds a push credential Secret.
// Credentials are never returned. Its name is the cluster's.
type RegistryBinding struct {
	Name     string `json:"name" yaml:"name"`
	Registry string `json:"registry" yaml:"registry"`
	Cluster  string `json:"cluster" yaml:"cluster"`
	// PushNamespaces are the namespaces that hold a push credential Secret;
	// empty for a pull-only connection. The only field that can change.
	PushNamespaces []string `json:"pushNamespaces" yaml:"pushNamespaces"`
	// SecretName is the push credential Secret's name in each push
	// namespace. It cannot change.
	SecretName      string                 `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	Status          *RegistryBindingStatus `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string                 `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string                 `json:"createdAt" yaml:"createdAt"`
}

// RegistryBindingStatus is the observed state of a cluster connection.
// Phase: Pending | Ready | Failed; Ready only when pull works and every
// push namespace is Ready.
type RegistryBindingStatus struct {
	Phase   string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
	// PullSecretName is the platform-side Secret the cluster's pods pull
	// with; it never enters the cluster.
	PullSecretName string `json:"pullSecretName,omitempty" yaml:"pullSecretName,omitempty"`
	PullUsername   string `json:"pullUsername,omitempty" yaml:"pullUsername,omitempty"`
	PushUsername   string `json:"pushUsername,omitempty" yaml:"pushUsername,omitempty"`
	// Namespaces is what was delivered into each push namespace. A
	// namespace removed from PushNamespaces stays here until its Secret is
	// withdrawn.
	Namespaces []RegistryBindingNamespaceStatus `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	Rotation   *RegistryBindingRotationStatus   `json:"rotation,omitempty" yaml:"rotation,omitempty"`
	Conditions []Condition                      `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// Conflicted reports whether the connection's Conflict condition is True:
// a push namespace holds a Secret of the push Secret's name that the
// platform did not create.
func (s *RegistryBindingStatus) Conflicted() bool {
	return s != nil && conditionTrue(s.Conditions, bindingConditionConflict)
}

// RegistryBindingNamespaceStatus is what a connection delivered into one
// push namespace. Phase: Pending | Ready | Failed.
type RegistryBindingNamespaceStatus struct {
	Name    string `json:"name" yaml:"name"`
	Phase   string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// RegistryBindingRotationStatus is a connection's credential rotation state.
// The values are opaque tokens: compare them for equality only.
type RegistryBindingRotationStatus struct {
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
	AppliedAt   string `json:"appliedAt" yaml:"appliedAt"`
	Pending     bool   `json:"pending" yaml:"pending"`
}

// CreateRegistryBindingRequest is the POST body. Name must equal Cluster;
// an empty SecretName takes the platform default (kupe-registry-push).
type CreateRegistryBindingRequest struct {
	Name           string   `json:"name"`
	Cluster        string   `json:"cluster"`
	PushNamespaces []string `json:"pushNamespaces,omitempty"`
	SecretName     string   `json:"secretName,omitempty"`
}

// PatchRegistryBindingRequest is the PATCH body: the complete new list of
// push namespaces (an empty list removes every push credential). The push
// Secret name cannot change.
type PatchRegistryBindingRequest struct {
	PushNamespaces *[]string `json:"pushNamespaces,omitempty"`
}

// RegistryBindingRotation acknowledges a rotate request. The platform
// replaces the connection's pull and push credentials asynchronously; wait
// for status.rotation.appliedAt to equal RequestedAt.
type RegistryBindingRotation struct {
	Binding     string `json:"binding" yaml:"binding"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
}

// Page is one page of a paged list: Total counts every item across pages.
type Page[T any] struct {
	Items    []T `json:"items" yaml:"items"`
	Page     int `json:"page" yaml:"page"`
	PageSize int `json:"pageSize" yaml:"pageSize"`
	Total    int `json:"total" yaml:"total"`
}

// MaxRegistryPageSize is the largest page kupe-api serves on the repository
// and image lists.
const MaxRegistryPageSize = 100

// Repository is one repository of the registry. Name is without the
// registry host and tenant ("team-a/api").
type Repository struct {
	Name string `json:"name" yaml:"name"`
	// ImageCount counts every image, each platform image of a
	// multi-platform image as well as the multi-platform image itself.
	ImageCount int64  `json:"imageCount" yaml:"imageCount"`
	PullCount  int64  `json:"pullCount" yaml:"pullCount"`
	CreatedAt  string `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
	// PullReference is what docker pull takes, before ":<tag>" or
	// "@<digest>".
	PullReference string `json:"pullReference" yaml:"pullReference"`
}

// Image is one image of a repository: a single-platform image, a
// multi-platform image (Type index), a Helm chart or another OCI artefact.
type Image struct {
	Repository   string          `json:"repository" yaml:"repository"`
	Digest       string          `json:"digest" yaml:"digest"`
	Type         string          `json:"type" yaml:"type"`
	MediaType    string          `json:"mediaType" yaml:"mediaType"`
	ArtifactType string          `json:"artifactType" yaml:"artifactType"`
	Size         int64           `json:"size" yaml:"size"`
	PushedAt     string          `json:"pushedAt,omitempty" yaml:"pushedAt,omitempty"`
	LastPulledAt string          `json:"lastPulledAt,omitempty" yaml:"lastPulledAt,omitempty"`
	Tags         []ImageTag      `json:"tags" yaml:"tags"`
	Platforms    []ImagePlatform `json:"platforms" yaml:"platforms"`
	Pull         ImagePull       `json:"pull" yaml:"pull"`
	Links        ImageLinks      `json:"links" yaml:"links"`
}

// ImageTag is one tag of an image.
type ImageTag struct {
	Name         string `json:"name" yaml:"name"`
	PushedAt     string `json:"pushedAt,omitempty" yaml:"pushedAt,omitempty"`
	LastPulledAt string `json:"lastPulledAt,omitempty" yaml:"lastPulledAt,omitempty"`
}

// ImagePlatform is one platform of an image.
type ImagePlatform struct {
	OS           string `json:"os" yaml:"os"`
	Architecture string `json:"architecture" yaml:"architecture"`
	Variant      string `json:"variant" yaml:"variant"`
	Digest       string `json:"digest" yaml:"digest"`
}

// ImagePull is what docker pull takes for an image: by digest, and by each
// tag.
type ImagePull struct {
	ByDigest string   `json:"byDigest" yaml:"byDigest"`
	ByTag    []string `json:"byTag" yaml:"byTag"`
}

// ImageLinks holds the image's canonical API URL, always by digest.
type ImageLinks struct {
	Self string `json:"self" yaml:"self"`
}

// ListRepositoriesOptions narrows a repository list. Zero values take the
// API's defaults (page 1, 25 a page, no search).
type ListRepositoriesOptions struct {
	Search   string
	Page     int
	PageSize int
}

// ListImagesOptions narrows an image list. A nil Tagged lists every image;
// true only tagged ones, false only untagged ones.
type ListImagesOptions struct {
	Tagged   *bool
	Page     int
	PageSize int
}

// Access token kinds and access levels.
const (
	// RegistryTokenKindToken is an access token, made for automation.
	RegistryTokenKindToken = "token"
	// RegistryTokenKindSession is a login session from kupe registry login.
	RegistryTokenKindSession = "session"

	RegistryAccessPull = "pull"
	// RegistryAccessPush includes pull.
	RegistryAccessPush = "push"
)

// RegistryToken is an access token or a login session, never with its
// secret.
type RegistryToken struct {
	// ID is rt- and 16 hex characters.
	ID          string `json:"id" yaml:"id"`
	DisplayName string `json:"displayName" yaml:"displayName"`
	Kind        string `json:"kind" yaml:"kind"`
	Access      string `json:"access" yaml:"access"`
	// CreatedBy is the email of the member who created it.
	CreatedBy string `json:"createdBy" yaml:"createdBy"`
	CreatedAt string `json:"createdAt" yaml:"createdAt"`
	ExpiresAt string `json:"expiresAt" yaml:"expiresAt"`
	// Username is the registry username, empty until the token is issued.
	Username   string               `json:"username" yaml:"username"`
	LastUsedAt string               `json:"lastUsedAt,omitempty" yaml:"lastUsedAt,omitempty"`
	Status     *RegistryTokenStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// RegistryTokenStatus is a token's observed state. Phase: Pending until the
// token is issued (seconds), then Ready; Failed means it can never work.
type RegistryTokenStatus struct {
	Phase      string      `json:"phase,omitempty" yaml:"phase,omitempty"`
	Message    string      `json:"message,omitempty" yaml:"message,omitempty"`
	Conditions []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// Phase is the token's phase, "" while it has no status.
func (t *RegistryToken) Phase() string {
	if t == nil || t.Status == nil {
		return ""
	}
	return t.Status.Phase
}

// CreatedRegistryToken is the create response of an access token or a login
// session: the token with its secret, returned this once, and the registry
// host to sign in to.
type CreatedRegistryToken struct {
	RegistryToken `yaml:",inline"`
	// Registry is the registry host to sign in to ("registry.kupe.cloud").
	Registry string `json:"registry" yaml:"registry"`
	// Secret is the password for the registry (kupecr_...).
	Secret string `json:"secret" yaml:"secret"`
}

// CreateRegistryTokenRequest is the access token POST body. A nil
// ExpiresInDays takes the platform default (90 days).
type CreateRegistryTokenRequest struct {
	DisplayName   string `json:"displayName"`
	Access        string `json:"access"`
	ExpiresInDays *int   `json:"expiresInDays,omitempty"`
}

// Error codes the registry endpoints return, lower-cased as ErrorCode
// returns them. Compare via ErrorCode(err).
const (
	// RegistryCodeUnavailable: 404 when the registry is switched off on the
	// platform, or kupe-api has no connection to it for repository, image
	// and tag routes. Listing, getting and deleting the registry, turning
	// its deletion protection off, and removing cluster connections still
	// work so it can be cleaned up.
	RegistryCodeUnavailable = "registries_unavailable"
	// RegistryCodeNotReady: 409 from a repository, image or tag route while
	// the registry is not provisioned yet.
	RegistryCodeNotReady = "registry_not_ready"
	// RegistryCodeDeletionProtected: 409 from DELETE while the registry has
	// deletion protection on (the same code as databases).
	RegistryCodeDeletionProtected = "deletion_protected"
	// RegistryCodeAlreadyExists: 409 from POST; a tenant has one registry.
	RegistryCodeAlreadyExists = "registry_already_exists"
	// RegistryCodeStorageLimitAbovePlan: 400 for a storage limit above the
	// plan's cap.
	RegistryCodeStorageLimitAbovePlan = "registry_storage_limit_above_plan"
	// RegistryCodeBindingExists: 409 for connecting a cluster that is
	// already connected.
	RegistryCodeBindingExists = "registry_binding_exists"
	// RegistryCodePushNamespacesLimit: 400 for more push namespaces in one
	// connection than the platform allows (10).
	RegistryCodePushNamespacesLimit = "registry_push_namespaces_limit"
	// RegistryCodePushNamespacesServiceLimit: 400 for more push namespaces
	// across all of the registry's connections than the platform allows
	// (20).
	RegistryCodePushNamespacesServiceLimit = "registry_push_namespaces_service_limit"
	// RegistryCodeTokenPushAdminOnly: 403 for a push token created by a
	// member who is not an admin.
	RegistryCodeTokenPushAdminOnly = "registry_token_push_admin_only" // #nosec G101 -- an error code, not a credential
	// RegistryCodeTokenExpiryInvalid: 400 for an expiry out of range.
	RegistryCodeTokenExpiryInvalid = "registry_token_expiry_invalid" // #nosec G101 -- an error code, not a credential
	// RegistryCodeTokenLimitReached: 409 for an access token over the
	// tenant's limit (50; login sessions are not counted).
	RegistryCodeTokenLimitReached = "registry_token_limit_reached" // #nosec G101 -- an error code, not a credential
	// RegistryCodeImageInUse: 409 for deleting a platform image a
	// multi-platform image still contains.
	RegistryCodeImageInUse = "image_in_use"
	// RegistryCodeImageProtected: 409 for an image or tag the registry
	// refuses to delete.
	RegistryCodeImageProtected = "image_protected"
	// RegistryCodeSessionRequiresUser: 403 for a login session requested
	// with an API key.
	RegistryCodeSessionRequiresUser = "session_requires_user"
)

// registryNotFoundText is kupe-api's 404 sentence for a registry that does
// not exist, on every registry route. Lower-case for matching.
var registryNotFoundText = strings.ToLower(`registry "` + RegistryName + `" not found`)

// IsRegistryMissing reports whether err is kupe-api saying the tenant has no
// registry: a 404 without a code whose message is the registry's own
// not-found sentence. A 404 for a repository, image, tag, connection or
// token names that instead.
func IsRegistryMissing(err error) bool {
	e := asAPI(err)
	return e != nil && e.StatusCode == http.StatusNotFound && e.Code == "" &&
		strings.Contains(strings.ToLower(e.Message), registryNotFoundText)
}

// registryPath is a path under the tenant's registry.
func (c *Client) registryPath(registry string, segments ...string) string {
	return c.tenantPath(append([]string{"registries", registry}, segments...)...)
}

// withQuery appends q to path when it has any values.
func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// pageQuery sets page and pageSize when they are positive.
func pageQuery(q url.Values, page, pageSize int) {
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
}

// RotateRegistryBinding requests new pull and push credentials for a
// cluster connection. The API answers 202; the old credentials stop working
// once the platform has replaced them.
func (c *Client) RotateRegistryBinding(ctx context.Context, registry, binding string) (*RegistryBindingRotation, error) {
	var rotation RegistryBindingRotation
	if _, err := c.request(ctx, http.MethodPost, c.registryPath(registry, "bindings", binding, "rotate"), nil, &rotation); err != nil {
		return nil, err
	}
	return &rotation, nil
}

// ListRepositories returns one page of the registry's repositories.
func (c *Client) ListRepositories(ctx context.Context, registry string, opts ListRepositoriesOptions) (*Page[Repository], error) {
	q := url.Values{}
	if opts.Search != "" {
		q.Set("search", opts.Search)
	}
	pageQuery(q, opts.Page, opts.PageSize)
	var page Page[Repository]
	if _, err := c.request(ctx, http.MethodGet, withQuery(c.registryPath(registry, "repositories"), q), nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetRepository returns one repository. repository may contain "/"; it is
// sent as one path segment ("team-a%2Fapi").
func (c *Client) GetRepository(ctx context.Context, registry, repository string) (*Repository, error) {
	var repo Repository
	if _, err := c.request(ctx, http.MethodGet, c.registryPath(registry, "repositories", repository), nil, &repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

// DeleteRepository deletes a repository with every image and tag in it.
func (c *Client) DeleteRepository(ctx context.Context, registry, repository string) error {
	_, err := c.request(ctx, http.MethodDelete, c.registryPath(registry, "repositories", repository), nil, nil)
	return err
}

// ListImages returns one page of a repository's images.
func (c *Client) ListImages(ctx context.Context, registry, repository string, opts ListImagesOptions) (*Page[Image], error) {
	q := url.Values{}
	if opts.Tagged != nil {
		q.Set("tagged", strconv.FormatBool(*opts.Tagged))
	}
	pageQuery(q, opts.Page, opts.PageSize)
	var page Page[Image]
	if _, err := c.request(ctx, http.MethodGet, withQuery(c.registryPath(registry, "repositories", repository, "images"), q), nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetImage returns one image by digest ("sha256:...") or tag.
func (c *Client) GetImage(ctx context.Context, registry, repository, reference string) (*Image, error) {
	var img Image
	if _, err := c.request(ctx, http.MethodGet, c.registryPath(registry, "repositories", repository, "images", reference), nil, &img); err != nil {
		return nil, err
	}
	return &img, nil
}

// DeleteImage deletes one image by digest, with every tag on it. The API
// refuses a tag here; DeleteTag removes one tag.
func (c *Client) DeleteImage(ctx context.Context, registry, repository, digest string) error {
	_, err := c.request(ctx, http.MethodDelete, c.registryPath(registry, "repositories", repository, "images", digest), nil, nil)
	return err
}

// DeleteTag removes one tag; the image stays.
func (c *Client) DeleteTag(ctx context.Context, registry, repository, tag string) error {
	_, err := c.request(ctx, http.MethodDelete, c.registryPath(registry, "repositories", repository, "tags", tag), nil, nil)
	return err
}

// ListRegistryTokens returns the registry's access tokens and login
// sessions, newest first; kind ("token" or "session") narrows it, "" lists
// both.
func (c *Client) ListRegistryTokens(ctx context.Context, registry, kind string) ([]RegistryToken, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	var resp struct {
		Items []RegistryToken `json:"items"`
	}
	if _, err := c.request(ctx, http.MethodGet, withQuery(c.registryPath(registry, "tokens"), q), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetRegistryToken returns one access token or login session.
func (c *Client) GetRegistryToken(ctx context.Context, registry, id string) (*RegistryToken, error) {
	var token RegistryToken
	if _, err := c.request(ctx, http.MethodGet, c.registryPath(registry, "tokens", id), nil, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// CreateRegistryToken creates an access token. The response carries its
// secret, this once; the token works once its phase is Ready.
func (c *Client) CreateRegistryToken(ctx context.Context, registry string, req CreateRegistryTokenRequest) (*CreatedRegistryToken, error) {
	var created CreatedRegistryToken
	if _, err := c.request(ctx, http.MethodPost, c.registryPath(registry, "tokens"), req, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// DeleteRegistryToken revokes an access token or login session.
func (c *Client) DeleteRegistryToken(ctx context.Context, registry, id string) error {
	_, err := c.request(ctx, http.MethodDelete, c.registryPath(registry, "tokens", id), nil, nil)
	return err
}

// CreateRegistrySession creates a login session for the signed-in member: 12
// hours, push for admins and pull for read-only members. An API key is
// refused (RegistryCodeSessionRequiresUser). The response carries the
// secret, this once; the session works once its phase is Ready.
func (c *Client) CreateRegistrySession(ctx context.Context, registry string) (*CreatedRegistryToken, error) {
	var created CreatedRegistryToken
	if _, err := c.request(ctx, http.MethodPost, c.registryPath(registry, "sessions"), nil, &created); err != nil {
		return nil, err
	}
	return &created, nil
}
