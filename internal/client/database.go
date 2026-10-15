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

// EffectiveSecretName is the credentials Secret name in the namespace: the
// one the platform reports, else the one requested.
func (b *DatabaseBinding) EffectiveSecretName() string {
	if b.Status != nil && b.Status.SecretName != "" {
		return b.Status.SecretName
	}
	return b.SecretName
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

// bindingConditionConflict is True while an object the platform did not
// create holds the binding's Service or Secret name in the namespace.
const bindingConditionConflict = "Conflict"

// Conflicted reports whether the binding's Conflict condition is True.
func (s *DatabaseBindingStatus) Conflicted() bool {
	if s == nil {
		return false
	}
	for _, c := range s.Conditions {
		if c.Type == bindingConditionConflict {
			return c.Status == "True"
		}
	}
	return false
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

// bindings is the binding collection of one database. Bindings are never
// patched; delete one and add another to change it.
func (c *Client) bindings(database string) resource[DatabaseBinding, CreateDatabaseBindingRequest, struct{}] {
	return newResource[DatabaseBinding, CreateDatabaseBindingRequest, struct{}](c, "databases", database, "bindings")
}

// ListDatabaseBindings returns the bindings of one database. A missing
// database is a 404, not an empty list.
func (c *Client) ListDatabaseBindings(ctx context.Context, database string) ([]DatabaseBinding, error) {
	return c.bindings(database).List(ctx)
}

// GetDatabaseBinding returns one binding of a database.
func (c *Client) GetDatabaseBinding(ctx context.Context, database, name string) (*DatabaseBinding, error) {
	b, _, err := c.bindings(database).Get(ctx, name)
	return b, err
}

// CreateDatabaseBinding binds a database into a cluster namespace.
func (c *Client) CreateDatabaseBinding(ctx context.Context, database string, req CreateDatabaseBindingRequest) (*DatabaseBinding, error) {
	b, _, err := c.bindings(database).Create(ctx, req)
	return b, err
}

// DeleteDatabaseBinding removes a binding. The role's password is not
// changed; rotate it to revoke what the namespace already knows.
func (c *Client) DeleteDatabaseBinding(ctx context.Context, database, name string) error {
	return c.bindings(database).Delete(ctx, name)
}
