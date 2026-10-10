package client

import (
	"context"
	"net/http"
)

// Database is the API response shape for a managed database. Field names
// mirror kupe-api's database response. It never carries credentials: role
// passwords reach applications only through bindings.
type Database struct {
	Name    string `json:"name" yaml:"name"`
	Engine  string `json:"engine" yaml:"engine"`
	Version string `json:"version" yaml:"version"`
	// Resources are per instance; a highly available database runs two.
	Resources          *DatabaseResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	HighAvailability   bool               `json:"highAvailability" yaml:"highAvailability"`
	DeletionProtection bool               `json:"deletionProtection" yaml:"deletionProtection"`
	Status             *DatabaseStatus    `json:"status,omitempty" yaml:"status,omitempty"`
	// Generation bumps on every spec change; compare with
	// status.observedGeneration to know when a change has been applied.
	Generation      int64  `json:"generation,omitempty" yaml:"generation,omitempty"`
	ResourceVersion string `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string `json:"createdAt" yaml:"createdAt"`
}

// DatabaseResources are the per-instance CPU, memory and storage a database
// requests. CPU has no unit ("500m", "2"); memory and storage need one
// ("1Gi", "10Gi").
type DatabaseResources struct {
	CPU     string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty" yaml:"memory,omitempty"`
	Storage string `json:"storage,omitempty" yaml:"storage,omitempty"`
}

// DatabaseStatus is the observed state. Phase values match clusters:
// Pending | Provisioning | Running | Upgrading | Degraded | Terminating.
type DatabaseStatus struct {
	ObservedGeneration int64  `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Phase              string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// CurrentVersion is the full engine version running, e.g. "18.0".
	CurrentVersion string `json:"currentVersion,omitempty" yaml:"currentVersion,omitempty"`
	// PreviousVersion is set while a major version upgrade is in progress.
	PreviousVersion  string      `json:"previousVersion,omitempty" yaml:"previousVersion,omitempty"`
	InstancesReady   int32       `json:"instancesReady" yaml:"instancesReady"`
	InstancesDesired int32       `json:"instancesDesired" yaml:"instancesDesired"`
	Conditions       []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// Condition is a Kubernetes-style status condition.
type Condition struct {
	Type               string `json:"type" yaml:"type"`
	Status             string `json:"status" yaml:"status"`
	Reason             string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string `json:"message,omitempty" yaml:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty" yaml:"lastTransitionTime,omitempty"`
}

// CreateDatabaseRequest is the POST body. An empty Engine or Version takes
// the platform default; a nil DeletionProtection takes the default (on).
type CreateDatabaseRequest struct {
	Name               string             `json:"name"`
	Engine             string             `json:"engine,omitempty"`
	Version            string             `json:"version,omitempty"`
	Resources          *DatabaseResources `json:"resources"`
	HighAvailability   bool               `json:"highAvailability,omitempty"`
	DeletionProtection *bool              `json:"deletionProtection,omitempty"`
}

// PatchDatabaseRequest is the PATCH (JSON merge-patch) body. Nil fields are
// left unchanged; at least one must be set.
type PatchDatabaseRequest struct {
	Version            *string            `json:"version,omitempty"`
	Resources          *DatabaseResources `json:"resources,omitempty"`
	HighAvailability   *bool              `json:"highAvailability,omitempty"`
	DeletionProtection *bool              `json:"deletionProtection,omitempty"`
}

// DatabaseMutator is the UpdateDatabaseRMW callback. Return nil to skip the
// PATCH (no-op update).
type DatabaseMutator func(current *Database) *PatchDatabaseRequest

// Error codes the database endpoints return. Compare via ErrorCode(err).
const (
	// DatabaseCodeUnavailable: 404 when managed databases are not enabled
	// on the platform. Listing, getting and deleting databases, and turning
	// deletion protection off, still work so existing ones can be removed.
	DatabaseCodeUnavailable = "databases_unavailable"
	// DatabaseDeleteCodeProtected: 409 from DELETE while the database has
	// deletion protection on.
	DatabaseDeleteCodeProtected = "deletion_protected"
	// DatabaseCodeMajorUpgradeUnavailable: 400 from PATCH when the version
	// changes and the platform does not offer major upgrades.
	DatabaseCodeMajorUpgradeUnavailable = "major_upgrade_unavailable"
	// DatabaseCodeStorageShrinkUnsupported: 400 from PATCH when the
	// requested storage is smaller than the current size.
	DatabaseCodeStorageShrinkUnsupported = "storage_shrink_unsupported"
)

// Database roles: owner can change the schema, app can read and write data.
// Bindings grant one; credential rotation targets one.
const (
	DatabaseRoleOwner = "owner"
	DatabaseRoleApp   = "app"
)

// CredentialRotation acknowledges a rotate-credentials request. The
// platform rotates the password asynchronously; none is returned.
type CredentialRotation struct {
	Database    string `json:"database" yaml:"database"`
	Role        string `json:"role" yaml:"role"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
}

// DatabaseBinding grants one role of a database to one namespace of one
// cluster. Credentials are never returned.
type DatabaseBinding struct {
	Name      string `json:"name" yaml:"name"`
	Database  string `json:"database" yaml:"database"`
	Cluster   string `json:"cluster" yaml:"cluster"`
	Namespace string `json:"namespace" yaml:"namespace"`
	Role      string `json:"role" yaml:"role"`
	// SecretName as requested; empty means the platform default. The
	// effective name is in Status.SecretName.
	SecretName      string                 `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	Status          *DatabaseBindingStatus `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string                 `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string                 `json:"createdAt" yaml:"createdAt"`
}

// DatabaseBindingStatus is the observed state. Phase: Pending | Ready | Failed.
type DatabaseBindingStatus struct {
	Phase string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// Host is the in-cluster DNS name applications connect to.
	Host string `json:"host,omitempty" yaml:"host,omitempty"`
	// SecretName is the effective credentials Secret name in the namespace.
	SecretName string      `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	Message    string      `json:"message,omitempty" yaml:"message,omitempty"`
	Conditions []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// CreateDatabaseBindingRequest is the POST body. The database comes from
// the path.
type CreateDatabaseBindingRequest struct {
	Name       string `json:"name"`
	Cluster    string `json:"cluster"`
	Namespace  string `json:"namespace"`
	Role       string `json:"role"`
	SecretName string `json:"secretName,omitempty"`
}

// ListDatabases returns every managed database in the tenant.
func (c *Client) ListDatabases(ctx context.Context) ([]Database, error) {
	var resp struct {
		Items []Database `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("databases"), nil, &resp)
	return resp.Items, err
}

// GetDatabase returns one database and its ETag.
func (c *Client) GetDatabase(ctx context.Context, name string) (*Database, string, error) {
	var db Database
	etag, err := c.request(ctx, http.MethodGet, c.tenantPath("databases", name), nil, &db)
	if err != nil {
		return nil, "", err
	}
	return &db, etag, nil
}

// CreateDatabase posts a new database. Provisioning is asynchronous; poll
// GetDatabase until status.phase is Running.
func (c *Client) CreateDatabase(ctx context.Context, req CreateDatabaseRequest) (*Database, string, error) {
	var db Database
	etag, err := c.request(ctx, http.MethodPost, c.tenantPath("databases"), req, &db)
	if err != nil {
		return nil, "", err
	}
	return &db, etag, nil
}

// UpdateDatabase patches a database with If-Match optimistic locking; an
// empty etag skips the header.
func (c *Client) UpdateDatabase(ctx context.Context, name, etag string, req PatchDatabaseRequest) (*Database, string, error) {
	var db Database
	newETag, err := c.requestWithETag(ctx, http.MethodPatch, c.tenantPath("databases", name), etag, req, &db)
	if err != nil {
		return nil, "", err
	}
	return &db, newETag, nil
}

// UpdateDatabaseRMW is the read-modify-write helper for `database update`
// and `database upgrade`: one retry on 412, then ErrRMWContention.
func (c *Client) UpdateDatabaseRMW(ctx context.Context, name string, mutate DatabaseMutator) (*Database, error) {
	return updateRMW(ctx,
		func(ctx context.Context) (*Database, string, error) { return c.GetDatabase(ctx, name) },
		func(ctx context.Context, etag string, req PatchDatabaseRequest) (*Database, string, error) {
			return c.UpdateDatabase(ctx, name, etag, req)
		},
		mutate, ErrRMWContention)
}

// DeleteDatabase deletes a database. A database with deletion protection on
// is refused with 409 and ErrorCode DatabaseDeleteCodeProtected.
func (c *Client) DeleteDatabase(ctx context.Context, name string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("databases", name), nil, nil)
	return err
}

// RotateDatabaseCredentials requests a new password for one role
// (DatabaseRoleOwner or DatabaseRoleApp). The API answers 202; the old
// password stops working once the platform has rotated it.
func (c *Client) RotateDatabaseCredentials(ctx context.Context, name, role string) (*CredentialRotation, error) {
	var rotation CredentialRotation
	body := struct {
		Role string `json:"role"`
	}{Role: role}
	if _, err := c.request(ctx, http.MethodPost, c.tenantPath("databases", name, "rotate-credentials"), body, &rotation); err != nil {
		return nil, err
	}
	return &rotation, nil
}

// ListDatabaseBindings returns the bindings of one database. A missing
// database is a 404, not an empty list.
func (c *Client) ListDatabaseBindings(ctx context.Context, database string) ([]DatabaseBinding, error) {
	var resp struct {
		Items []DatabaseBinding `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("databases", database, "bindings"), nil, &resp)
	return resp.Items, err
}

// GetDatabaseBinding returns one binding of a database.
func (c *Client) GetDatabaseBinding(ctx context.Context, database, name string) (*DatabaseBinding, error) {
	var b DatabaseBinding
	if _, err := c.request(ctx, http.MethodGet, c.tenantPath("databases", database, "bindings", name), nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateDatabaseBinding binds a database into a cluster namespace.
func (c *Client) CreateDatabaseBinding(ctx context.Context, database string, req CreateDatabaseBindingRequest) (*DatabaseBinding, error) {
	var b DatabaseBinding
	if _, err := c.request(ctx, http.MethodPost, c.tenantPath("databases", database, "bindings"), req, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// DeleteDatabaseBinding removes a binding. The role's password is not
// changed; rotate it to revoke what the namespace already knows.
func (c *Client) DeleteDatabaseBinding(ctx context.Context, database, name string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("databases", database, "bindings", name), nil, nil)
	return err
}
