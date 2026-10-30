package client

import (
	"context"
	"net/http"
	"net/url"
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
	// Backup is nil while the database has never set its backup settings:
	// it keeps the platform defaults.
	Backup *DatabaseBackup `json:"backup,omitempty" yaml:"backup,omitempty"`
	// Source is what the database was restored from; nil for a database
	// that was not restored.
	Source *DatabaseSource `json:"source,omitempty" yaml:"source,omitempty"`
	Status *DatabaseStatus `json:"status,omitempty" yaml:"status,omitempty"`
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

// DatabaseBackup is a database's backup settings, sent on create and PATCH
// and returned on every read. Retention is days of backups kept; Schedule
// is how many full backups are kept within them (Daily or
// DailyThenWeekly). A nil Retention or an empty Schedule is not sent; on a
// response, a nil Retention was never set and the platform default
// applies.
type DatabaseBackup struct {
	Retention *int32 `json:"retention,omitempty" yaml:"retention,omitempty"`
	Schedule  string `json:"schedule,omitempty" yaml:"schedule,omitempty"`
}

// RecoveryWindow is the oldest and newest points a database can be
// recovered to, as RFC 3339 timestamps.
type RecoveryWindow struct {
	Earliest string `json:"earliest" yaml:"earliest"`
	Latest   string `json:"latest" yaml:"latest"`
}

// DatabaseStatus is the observed state. Phase values match clusters:
// Pending | Provisioning | Running | Upgrading | Degraded | Terminating.
type DatabaseStatus struct {
	ObservedGeneration int64  `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Phase              string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// CurrentVersion is the full engine version running, e.g. "18.0".
	CurrentVersion string `json:"currentVersion,omitempty" yaml:"currentVersion,omitempty"`
	// PreviousVersion is set while a major version upgrade is in progress.
	PreviousVersion  string `json:"previousVersion,omitempty" yaml:"previousVersion,omitempty"`
	InstancesReady   int32  `json:"instancesReady" yaml:"instancesReady"`
	InstancesDesired int32  `json:"instancesDesired" yaml:"instancesDesired"`
	// Rotations is each role's credential rotation state, keyed by role;
	// nil from a kupe-api that predates it.
	Rotations DatabaseRotations `json:"rotations,omitempty" yaml:"rotations,omitempty"`
	// LastBackupAt is when the newest full backup finished (RFC 3339);
	// empty until the first one completes.
	LastBackupAt string `json:"lastBackupAt,omitempty" yaml:"lastBackupAt,omitempty"`
	// RecoveryWindow is nil until the backups report one.
	RecoveryWindow *RecoveryWindow `json:"recoveryWindow,omitempty" yaml:"recoveryWindow,omitempty"`
	// BackupSizeBytes is the storage every full backup and the WAL kept for
	// the database take up, manual backups included; 0 until measured.
	BackupSizeBytes int64       `json:"backupSizeBytes,omitempty" yaml:"backupSizeBytes,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// DatabaseRotations is each role's credential rotation state, keyed by role
// (owner, readwrite, readonly). JSON and YAML output keep the keys the
// server sent.
type DatabaseRotations map[string]RotationStatus

// Role returns role's state and whether the server reported it. A missing
// key is treated like no rotation status: a kupe-api that does not report
// the role cannot be waited on.
func (r DatabaseRotations) Role(role string) (RotationStatus, bool) {
	state, ok := r[role]
	return state, ok
}

// RotationStatus is one role's credential rotation state. The values are
// the opaque tokens rotate-credentials returns as requestedAt: compare them
// for equality only. A rotation is in effect once AppliedAt equals the token
// of its request: the new password works, the old one no longer does, and
// every bound credentials Secret carries it.
type RotationStatus struct {
	// RequestedAt is the token of the last rotation requested; empty if
	// none ever was.
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
	// AppliedAt is the token of the last rotation the database applied.
	AppliedAt string `json:"appliedAt" yaml:"appliedAt"`
	// Pending is true while the last request is not yet applied.
	Pending bool `json:"pending" yaml:"pending"`
	// Delivered is true once the last request is applied and every binding
	// of the role carries the new password; nil from a kupe-api that
	// predates it (read as delivered once applied).
	Delivered *bool `json:"delivered,omitempty" yaml:"delivered,omitempty"`
}

// InEffect reports whether the rotation requested with token is applied
// and delivered to every binding of the role (applied alone for a kupe-api
// that does not report delivery).
func (r RotationStatus) InEffect(token string) bool {
	return r.AppliedAt == token && (r.Delivered == nil || *r.Delivered)
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
	// Backup nil takes the platform's backup defaults.
	Backup *DatabaseBackup `json:"backup,omitempty"`
	// Source, when set, restores the new database instead of starting it
	// empty. Version may then be empty: the platform runs the source's
	// major.
	Source *DatabaseSource `json:"source,omitempty"`
}

// DatabaseSource is what a new database is restored from: a manual backup
// (Backup), or another database (Database) at PointInTime inside its
// restore window, the latest point when empty. Exactly one of Backup and
// Database is set; PointInTime only with Database (RFC 3339).
type DatabaseSource struct {
	Backup      string `json:"backup,omitempty" yaml:"backup,omitempty"`
	Database    string `json:"database,omitempty" yaml:"database,omitempty"`
	PointInTime string `json:"pointInTime,omitempty" yaml:"pointInTime,omitempty"`
}

// PatchDatabaseRequest is the PATCH (JSON merge-patch) body. Nil fields are
// left unchanged; at least one must be set. Backup changes only the keys it
// sets.
type PatchDatabaseRequest struct {
	Version            *string            `json:"version,omitempty"`
	Resources          *DatabaseResources `json:"resources,omitempty"`
	HighAvailability   *bool              `json:"highAvailability,omitempty"`
	DeletionProtection *bool              `json:"deletionProtection,omitempty"`
	Backup             *DatabaseBackup    `json:"backup,omitempty"`
}

// IsEmpty reports whether the patch changes nothing. It names every field:
// TestPatchDatabaseRequest_IsEmptyCoversEveryField fails when a new one is
// left out.
func (p PatchDatabaseRequest) IsEmpty() bool {
	return p.Version == nil && p.Resources == nil && p.HighAvailability == nil &&
		p.DeletionProtection == nil && p.Backup == nil
}

// NeedsWait reports whether the patch changes something the platform has
// to apply to the running database before it is in effect: the version,
// resources, high availability or backup settings (the operator applies a
// backup change to the database's backup objects). Deletion protection is
// read from the spec when a delete comes in, so it is in effect once the
// PATCH lands. TestPatchDatabaseRequest_NeedsWaitCoversEveryField fails
// when a new field is in neither group.
func (p PatchDatabaseRequest) NeedsWait() bool {
	return p.Version != nil || p.Resources != nil || p.HighAvailability != nil || p.Backup != nil
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
	// DatabaseCodeNameTooLong: 400 from POST when the name is longer than
	// the tenant may use: at most 44 characters, fewer for a tenant name
	// over 12 characters. The platform sends DATABASE_NAME_TOO_LONG (field
	// metadata.name); ErrorCode lower-cases it.
	DatabaseCodeNameTooLong = "database_name_too_long"
	// DatabaseCodeBackupsUnavailable: 400 from POST and PATCH when the
	// request sets backup settings and the platform does not offer backups
	// (field spec.backup).
	DatabaseCodeBackupsUnavailable = "backups_unavailable"
)

// Error codes of manual backups and restores (kupe-api MDB2-1v). Compare
// via ErrorCode.
const (
	// BackupCodeDatabaseNotReady: 409 from a backup of a database that is
	// not running.
	BackupCodeDatabaseNotReady = "database_not_ready"
	// BackupCodeLimitReached: 409 from a backup of a database that already
	// keeps the most manual backups a database may (20).
	BackupCodeLimitReached = "backup_limit_reached"
	// RestoreCodeSourceNotFound: 400 from a restore whose backup or
	// database does not exist (field spec.source).
	RestoreCodeSourceNotFound = "restore_source_not_found"
	// RestoreCodeSourceNotReady: 409 from a restore of a backup that has
	// not completed, or of a database with no restore window yet.
	RestoreCodeSourceNotReady = "restore_source_not_ready"
	// RestoreCodePointOutsideWindow: 400 from a restore point outside the
	// source database's restore window (field spec.source.pointInTime).
	RestoreCodePointOutsideWindow = "restore_point_outside_window"
	// RestoreCodeVersionMismatch: 400 from a restore asking for a major
	// version other than the source's (field spec.version).
	RestoreCodeVersionMismatch = "restore_version_mismatch"
	// RestoreCodeStorageTooSmall: 400 from a restore with less storage
	// than the source had (field spec.resources.storage).
	RestoreCodeStorageTooSmall = "restore_storage_too_small"
)

// Backup schedules: how many full backups are kept within the retention.
// Daily keeps one for every day; DailyThenWeekly one for each of the last 7
// days and one a week before that.
const (
	DatabaseBackupScheduleDaily           = "Daily"
	DatabaseBackupScheduleDailyThenWeekly = "DailyThenWeekly"
)

// DatabaseBackupSchedules lists every backup schedule, in the order help
// text names them.
var DatabaseBackupSchedules = []string{DatabaseBackupScheduleDaily, DatabaseBackupScheduleDailyThenWeekly}

// Database roles: owner can change the schema, readwrite can read and write
// data, readonly can read data. Bindings grant one; credential rotation
// targets one.
const (
	DatabaseRoleOwner     = "owner"
	DatabaseRoleReadWrite = "readwrite"
	DatabaseRoleReadOnly  = "readonly"
)

// DatabaseRoles lists every database role, in the order help text names
// them.
var DatabaseRoles = []string{DatabaseRoleOwner, DatabaseRoleReadWrite, DatabaseRoleReadOnly}

// CredentialRotation acknowledges a rotate-credentials request. The
// platform rotates the password asynchronously; none is returned. Wait for
// status.rotations.{role}.appliedAt to equal RequestedAt.
type CredentialRotation struct {
	Database    string `json:"database" yaml:"database"`
	Role        string `json:"role" yaml:"role"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
}

// DatabaseBinding grants one role of a database to namespaces of one
// cluster. Credentials are never returned.
type DatabaseBinding struct {
	Name     string `json:"name" yaml:"name"`
	Database string `json:"database" yaml:"database"`
	Cluster  string `json:"cluster" yaml:"cluster"`
	// Namespaces are the namespaces inside the cluster the binding delivers
	// into. The only field of a binding that can change.
	Namespaces []string `json:"namespaces" yaml:"namespaces"`
	Role       string   `json:"role" yaml:"role"`
	// SecretName as requested; empty means the platform default. The
	// effective name is in Status.SecretName.
	SecretName      string                 `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	Status          *DatabaseBindingStatus `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string                 `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string                 `json:"createdAt" yaml:"createdAt"`
}

// EffectiveSecretName is the credentials Secret name in the namespaces: the
// one the platform reports, else the one requested.
func (b *DatabaseBinding) EffectiveSecretName() string {
	if b.Status != nil && b.Status.SecretName != "" {
		return b.Status.SecretName
	}
	return b.SecretName
}

// BindingNamespace is one namespace of a binding as the platform reports it.
type BindingNamespace struct {
	// Name is the namespace inside the cluster.
	Name string
	// Listed is false for a namespace no longer in Namespaces whose Service
	// and Secret the platform is still withdrawing.
	Listed bool
	// Reported is false while the platform has not recorded the namespace
	// yet; Status is then the zero value.
	Reported bool
	Status   DatabaseBindingNamespaceStatus
}

// NamespaceStates lists every namespace of the binding: those in
// Namespaces in their order, then any the platform still reports but that
// are no longer listed (being withdrawn), in the order reported.
func (b *DatabaseBinding) NamespaceStates() []BindingNamespace {
	var reported []DatabaseBindingNamespaceStatus
	if b.Status != nil {
		reported = b.Status.Namespaces
	}
	out := make([]BindingNamespace, 0, len(b.Namespaces)+len(reported))
	listed := make(map[string]bool, len(b.Namespaces))
	for _, ns := range b.Namespaces {
		listed[ns] = true
		state := BindingNamespace{Name: ns, Listed: true}
		for _, st := range reported {
			if st.Name == ns {
				state.Reported, state.Status = true, st
				break
			}
		}
		out = append(out, state)
	}
	for _, st := range reported {
		if !listed[st.Name] {
			out = append(out, BindingNamespace{Name: st.Name, Reported: true, Status: st})
		}
	}
	return out
}

// DatabaseBindingStatus is the observed state. Phase: Pending | Ready |
// Failed; Ready only when every listed namespace is.
type DatabaseBindingStatus struct {
	Phase string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// SecretName is the effective credentials Secret name in every
	// namespace.
	SecretName string `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	Message    string `json:"message,omitempty" yaml:"message,omitempty"`
	// Namespaces is what was delivered into each namespace; empty until the
	// platform has reconciled the binding. A namespace removed from the
	// binding stays here until its Service and Secret are withdrawn.
	Namespaces []DatabaseBindingNamespaceStatus `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	Conditions []Condition                      `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// DatabaseBindingNamespaceStatus is what a binding delivered into one
// namespace. Phase: Pending | Ready | Failed.
type DatabaseBindingNamespaceStatus struct {
	Name string `json:"name" yaml:"name"`
	// Host is the in-cluster DNS name applications in the namespace
	// connect to; empty while nothing is delivered.
	Host  string `json:"host,omitempty" yaml:"host,omitempty"`
	Phase string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// DeliveredRotation is the rotate-credentials token of the password
	// the namespace's Secret carries; empty if the role was never rotated.
	DeliveredRotation string `json:"deliveredRotation,omitempty" yaml:"deliveredRotation,omitempty"`
	Message           string `json:"message,omitempty" yaml:"message,omitempty"`
}

// bindingConditionConflict is True while an object the platform did not
// create holds the binding's Service or Secret name in the namespace.
const bindingConditionConflict = "Conflict"

// Conflicted reports whether the binding's Conflict condition is True.
func (s *DatabaseBindingStatus) Conflicted() bool {
	return s != nil && conditionTrue(s.Conditions, bindingConditionConflict)
}

// conditionReady is True while every instance of a database is healthy on
// the spec last applied; it is False while a change rolls out, even though
// the phase stays Running.
const conditionReady = "Ready"

// Ready reports whether the database's Ready condition is True.
func (s *DatabaseStatus) Ready() bool {
	return s != nil && conditionTrue(s.Conditions, conditionReady)
}

// conditionTrue reports whether the condition of type t is True.
func conditionTrue(conditions []Condition, t string) bool {
	for _, c := range conditions {
		if c.Type == t {
			return c.Status == "True"
		}
	}
	return false
}

// CreateDatabaseBindingRequest is the POST body. The database comes from
// the path.
type CreateDatabaseBindingRequest struct {
	Name       string   `json:"name"`
	Cluster    string   `json:"cluster"`
	Namespaces []string `json:"namespaces"`
	Role       string   `json:"role"`
	SecretName string   `json:"secretName,omitempty"`
}

// RotateDatabaseCredentials requests a new password for one role (one of
// DatabaseRoles). The API answers 202; the old
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

// PatchDatabaseBindingRequest is the PATCH body: the binding's complete new
// namespace list. Namespaces are the only field of a binding that can
// change.
type PatchDatabaseBindingRequest struct {
	Namespaces []string `json:"namespaces"`
}

// DatabaseBindingMutator is the UpdateDatabaseBindingRMW callback: it gets
// the binding as read and returns the patch to send, or nil to send none.
type DatabaseBindingMutator func(current *DatabaseBinding) *PatchDatabaseBindingRequest

// bindings is the binding collection of one database.
func (c *Client) bindings(database string) resource[DatabaseBinding, CreateDatabaseBindingRequest, PatchDatabaseBindingRequest] {
	return newResource[DatabaseBinding, CreateDatabaseBindingRequest, PatchDatabaseBindingRequest](c, "databases", database, "bindings")
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

// CreateDatabaseBinding binds a database into namespaces of a cluster.
func (c *Client) CreateDatabaseBinding(ctx context.Context, database string, req CreateDatabaseBindingRequest) (*DatabaseBinding, error) {
	b, _, err := c.bindings(database).Create(ctx, req)
	return b, err
}

// UpdateDatabaseBindingRMW changes a binding's namespaces: it reads the
// binding, asks mutate for the new list and sends it with If-Match set to
// the ETag read. A binding changed in between (412) is read again and the
// change retried once, then ErrRMWContention. A nil patch sends nothing and
// returns the binding as read.
func (c *Client) UpdateDatabaseBindingRMW(ctx context.Context, database, name string, mutate DatabaseBindingMutator) (*DatabaseBinding, error) {
	return c.bindings(database).UpdateRMW(ctx, name, mutate)
}

// DeleteDatabaseBinding removes a binding. The role's password is not
// changed; rotate it to revoke what the namespace already knows.
func (c *Client) DeleteDatabaseBinding(ctx context.Context, database, name string) error {
	return c.bindings(database).Delete(ctx, name)
}

// Backup is a manual database backup: one the tenant took, or one the
// platform took before a major version change (Reason PreUpgrade). It
// outlives the database it was taken of and goes with the tenant. Not to
// be confused with DatabaseBackup, a database's automatic backup settings.
type Backup struct {
	Name string `json:"name" yaml:"name"`
	// Database is the name of the database the backup was taken of; it may
	// have been deleted since (Status.DatabaseDeleted).
	Database string `json:"database" yaml:"database"`
	// Reason is Manual or PreUpgrade.
	Reason          string        `json:"reason" yaml:"reason"`
	Status          *BackupStatus `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string        `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt       string        `json:"createdAt" yaml:"createdAt"`
}

// BackupStatus is a backup's observed state. Phase: Pending | Running |
// Completed | Failed | Deleting; a backup can be restored once Completed.
type BackupStatus struct {
	Phase string `json:"phase,omitempty" yaml:"phase,omitempty"`
	// CompletedAt is when the backup finished (RFC 3339); empty until then.
	CompletedAt string `json:"completedAt,omitempty" yaml:"completedAt,omitempty"`
	// SizeBytes is the backup's size; 0 until measured.
	SizeBytes int64 `json:"sizeBytes,omitempty" yaml:"sizeBytes,omitempty"`
	// Source is the database as it was when the backup started; nil until
	// it starts.
	Source *BackupSource `json:"source,omitempty" yaml:"source,omitempty"`
	// DatabaseDeleted is true once the database the backup was taken of is
	// deleted or being deleted.
	DatabaseDeleted bool        `json:"databaseDeleted" yaml:"databaseDeleted"`
	Conditions      []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// BackupSource is the database a backup was taken of. A database restored
// from the backup runs Version and needs at least Storage.
type BackupSource struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version" yaml:"version"`
	Storage string `json:"storage" yaml:"storage"`
}

// conditionMessage is the message of the condition of type t, "" when
// there is none.
func conditionMessage(conditions []Condition, t string) string {
	for _, c := range conditions {
		if c.Type == t {
			return c.Message
		}
	}
	return ""
}

// Message is the backup's Ready condition message: what it is waiting for
// or why it failed. "" when there is none.
func (s *BackupStatus) Message() string {
	if s == nil {
		return ""
	}
	return conditionMessage(s.Conditions, conditionReady)
}

// CreateBackupRequest is the optional POST body of a manual backup. An
// empty Name is not sent: the platform names the backup after the database
// and the time, in UTC.
type CreateBackupRequest struct {
	Name string `json:"name,omitempty"`
}

// ListDatabaseBackups returns the manual backups of one database, newest
// first. A missing database is a 404, not an empty list; the backups of an
// earlier database of the same name are not listed (ListBackups lists
// them).
func (c *Client) ListDatabaseBackups(ctx context.Context, database string) ([]Backup, error) {
	return c.listBackups(ctx, c.tenantPath("databases", database, "backups"))
}

// CreateDatabaseBackup takes a manual backup of a running database. The
// backup is taken asynchronously: poll GetBackup until Completed.
func (c *Client) CreateDatabaseBackup(ctx context.Context, database string, req CreateBackupRequest) (*Backup, error) {
	var b Backup
	if _, err := c.request(ctx, http.MethodPost, c.tenantPath("databases", database, "backups"), req, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBackups returns every manual backup of the tenant, newest first,
// deleted databases' included. A non-empty database lists only the
// backups taken of databases with that name.
func (c *Client) ListBackups(ctx context.Context, database string) ([]Backup, error) {
	q := url.Values{}
	if database != "" {
		q.Set("database", database)
	}
	return c.listBackups(ctx, withQuery(c.tenantPath("backups"), q))
}

func (c *Client) listBackups(ctx context.Context, path string) ([]Backup, error) {
	var resp struct {
		Items []Backup `json:"items"`
	}
	if _, err := c.request(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetBackup returns one manual backup.
func (c *Client) GetBackup(ctx context.Context, name string) (*Backup, error) {
	var b Backup
	if _, err := c.request(ctx, http.MethodGet, c.tenantPath("backups", name), nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// DeleteBackup deletes a manual backup; it can no longer be restored from.
// A backup already being deleted answers 204 as well.
func (c *Client) DeleteBackup(ctx context.Context, name string) error {
	_, err := c.request(ctx, http.MethodDelete, c.tenantPath("backups", name), nil, nil)
	return err
}
