package client

import "context"

// Interface is the surface commands depend on. Commands never depend on
// *Client directly — always through this interface, so tests can inject
// clienttest.Fake. New endpoints added in later phases extend this
// interface; each new method is a separate commit so the minimal surface
// stays visible in history.
type Interface interface {
	// Tenant returns the tenant name the client is scoped to. Used by
	// commands that want to display "acting as tenant X" without issuing a
	// network call.
	Tenant() string

	// GetTenant fetches /api/v1/tenants/{tenant}.
	GetTenant(ctx context.Context) (*Tenant, string, error)

	// DeleteTenant requests deletion of the tenant the client is scoped
	// to (owner + OIDC only server-side). Returns the 202 body — the
	// tenant with status.phase=Terminating.
	DeleteTenant(ctx context.Context, req DeleteTenantRequest) (*Tenant, error)

	// Clusters is the cluster collection.
	Clusters() ResourceAPI[Cluster, CreateClusterRequest, PatchClusterRequest]

	// GetClusterKubeconfig fetches the {endpoint, certificateAuthority}
	// envelope for a cluster. The full kubeconfig YAML is assembled
	// locally in internal/kubeconfig.
	GetClusterKubeconfig(ctx context.Context, name string) (*ClusterKubeconfig, error)

	// ListAPIKeys lists all API keys for the tenant (metadata only; the
	// raw key value is never returned). Admin-only server-side.
	ListAPIKeys(ctx context.Context) ([]APIKey, error)

	// CreateAPIKey creates a new API key. The response's Key field carries
	// the raw kupe_... token and is only populated on creation. Admin-only.
	CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (*APIKey, error)

	// DeleteAPIKey revokes an API key by ID. Admin-only.
	DeleteAPIKey(ctx context.Context, id string) error

	// ListSecrets lists every managed secret in the tenant.
	ListSecrets(ctx context.Context) ([]Secret, error)

	// GetSecret returns a single managed secret + its ETag.
	GetSecret(ctx context.Context, name string) (*Secret, string, error)

	// CreateSecret creates a new managed secret.
	CreateSecret(ctx context.Context, req CreateSecretRequest) (*Secret, string, error)

	// UpdateSecret patches a secret's sync targets with If-Match optimistic
	// locking.
	UpdateSecret(ctx context.Context, name, etag string, req PatchSecretRequest) (*Secret, string, error)

	// UpdateSecretRMW is the safe read-modify-write helper for
	// `secret update`; one retry on 412 before ErrSecretRMWContention.
	UpdateSecretRMW(ctx context.Context, name string, mutate SecretMutator) (*Secret, error)

	// DeleteSecret revokes a managed secret.
	DeleteSecret(ctx context.Context, name string) error

	// Databases is the database collection (Delete is refused with 409
	// DatabaseDeleteCodeProtected while deletion protection is on).
	Databases() ResourceAPI[Database, CreateDatabaseRequest, PatchDatabaseRequest]

	// RotateDatabaseCredentials requests a new password for one role.
	RotateDatabaseCredentials(ctx context.Context, name, role string) (*CredentialRotation, error)

	// ListDatabaseBindings lists the bindings of one database.
	ListDatabaseBindings(ctx context.Context, database string) ([]DatabaseBinding, error)

	// GetDatabaseBinding returns one binding of a database.
	GetDatabaseBinding(ctx context.Context, database, name string) (*DatabaseBinding, error)

	// CreateDatabaseBinding binds a database into namespaces of a cluster.
	CreateDatabaseBinding(ctx context.Context, database string, req CreateDatabaseBindingRequest) (*DatabaseBinding, error)

	// UpdateDatabaseBindingRMW changes a binding's namespaces with a
	// read-modify-write cycle; one retry on 412 before ErrRMWContention.
	UpdateDatabaseBindingRMW(ctx context.Context, database, name string, mutate DatabaseBindingMutator) (*DatabaseBinding, error)

	// DeleteDatabaseBinding removes a binding.
	DeleteDatabaseBinding(ctx context.Context, database, name string) error

	// ListDatabaseBackups lists the manual backups of one database.
	ListDatabaseBackups(ctx context.Context, database string) ([]Backup, error)

	// CreateDatabaseBackup takes a manual backup of a database.
	CreateDatabaseBackup(ctx context.Context, database string, req CreateBackupRequest) (*Backup, error)

	// ListBackups lists every manual backup of the tenant, deleted
	// databases' included; a non-empty database narrows it to the backups
	// of databases with that name.
	ListBackups(ctx context.Context, database string) ([]Backup, error)

	// GetBackup returns one manual backup.
	GetBackup(ctx context.Context, name string) (*Backup, error)

	// DeleteBackup deletes a manual backup.
	DeleteBackup(ctx context.Context, name string) error

	// Registries is the tenant's registry collection: at most one, always
	// named RegistryName (Delete is refused with 409
	// RegistryCodeDeletionProtected while deletion protection is on).
	Registries() ResourceAPI[Registry, CreateRegistryRequest, PatchRegistryRequest]

	// RegistryBindings is the cluster connection collection of a registry.
	RegistryBindings(registry string) ResourceAPI[RegistryBinding, CreateRegistryBindingRequest, PatchRegistryBindingRequest]

	// RotateRegistryBinding requests new pull and push credentials for a
	// cluster connection.
	RotateRegistryBinding(ctx context.Context, registry, binding string) (*RegistryBindingRotation, error)

	// ListRepositories returns one page of the registry's repositories.
	ListRepositories(ctx context.Context, registry string, opts ListRepositoriesOptions) (*Page[Repository], error)

	// GetRepository returns one repository.
	GetRepository(ctx context.Context, registry, repository string) (*Repository, error)

	// DeleteRepository deletes a repository with every image and tag in it.
	DeleteRepository(ctx context.Context, registry, repository string) error

	// ListImages returns one page of a repository's images.
	ListImages(ctx context.Context, registry, repository string, opts ListImagesOptions) (*Page[Image], error)

	// GetImage returns one image by digest or tag.
	GetImage(ctx context.Context, registry, repository, reference string) (*Image, error)

	// DeleteImage deletes one image by digest.
	DeleteImage(ctx context.Context, registry, repository, digest string) error

	// DeleteTag removes one tag; the image stays.
	DeleteTag(ctx context.Context, registry, repository, tag string) error

	// ListRegistryTokens lists access tokens and login sessions; kind
	// narrows it ("" lists both).
	ListRegistryTokens(ctx context.Context, registry, kind string) ([]RegistryToken, error)

	// GetRegistryToken returns one access token or login session.
	GetRegistryToken(ctx context.Context, registry, id string) (*RegistryToken, error)

	// CreateRegistryToken creates an access token; the response carries
	// its secret, this once. Admin-only for push.
	CreateRegistryToken(ctx context.Context, registry string, req CreateRegistryTokenRequest) (*CreatedRegistryToken, error)

	// DeleteRegistryToken revokes an access token or login session.
	DeleteRegistryToken(ctx context.Context, registry, id string) error

	// CreateRegistrySession creates a 12-hour login session for the
	// signed-in member (OIDC only); the response carries its secret, this
	// once.
	CreateRegistrySession(ctx context.Context, registry string) (*CreatedRegistryToken, error)

	// ListMembers lists every tenant member.
	ListMembers(ctx context.Context) ([]Member, error)

	// AddMember adds a member (email + role) to the tenant.
	AddMember(ctx context.Context, req AddMemberRequest) (*Member, error)

	// UpdateMember changes a member's role.
	UpdateMember(ctx context.Context, email string, req UpdateMemberRequest) (*Member, error)

	// RemoveMember removes a member.
	RemoveMember(ctx context.Context, email string) error

	// ListInvoices returns all invoices for the tenant (read-only; both
	// admin and readonly roles can call).
	ListInvoices(ctx context.Context) ([]Invoice, error)

	// GetInvoice fetches a single invoice by name (billing period).
	GetInvoice(ctx context.Context, name string) (*Invoice, error)

	// ListPlans returns the public platform plan catalog (unauthenticated
	// server-side; the CLI still carries the Authorization header for
	// operational simplicity).
	ListPlans(ctx context.Context) ([]Plan, error)

	// GetPlan fetches a single plan by name.
	GetPlan(ctx context.Context, name string) (*Plan, error)
}
