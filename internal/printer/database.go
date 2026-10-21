package printer

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// Database renders databases.
var Database = Resource[client.Database]{
	Columns:       databaseColumns,
	DetailColumns: databaseDetailColumns,
	Name:          func(db *client.Database) string { return db.Name },
}

// DatabaseBinding renders database bindings. Tables show one row per
// namespace; every other format renders the bindings as the API returns
// them.
var DatabaseBinding = bindingResource{Resource[client.DatabaseBinding]{
	Columns:       databaseBindingColumns,
	DetailColumns: databaseBindingDetailColumns,
	Name:          func(b *client.DatabaseBinding) string { return b.Name },
}}

// bindingResource is a Resource whose table views expand each binding into
// its namespaces.
type bindingResource struct {
	Resource[client.DatabaseBinding]
}

// List writes bindings to io.Out; a table has one row per namespace of
// each binding.
func (r bindingResource) List(io *cli.IOStreams, format *Format, items []client.DatabaseBinding) error {
	if format.Kind != Table && format.Kind != Wide {
		return r.Resource.List(io, format, items)
	}
	rows := make([]bindingRow, 0, len(items))
	for i := range items {
		rows = append(rows, bindingRows(&items[i])...)
	}
	return PrintTable(io.Out, rows, r.Columns(io.ColorEnabled), format.Kind == Wide)
}

// One writes one binding to io.Out; the detail view ends with a table of
// its namespaces.
func (r bindingResource) One(io *cli.IOStreams, format *Format, item *client.DatabaseBinding) error {
	if format.Kind != Table && format.Kind != Wide {
		return r.Resource.One(io, format, item)
	}
	if err := PrintDetails(io.Out, item, r.DetailColumns(io.ColorEnabled)); err != nil {
		return err
	}
	fmt.Fprintln(io.Out)
	return PrintTable(io.Out, bindingRows(item), bindingNamespaceColumns(io.ColorEnabled), true)
}

// bindingRow is one namespace of one binding in a table.
type bindingRow struct {
	binding   *client.DatabaseBinding
	namespace client.BindingNamespace
}

// bindingRows is a row per namespace of b, or a single row with no
// namespace if it lists none.
func bindingRows(b *client.DatabaseBinding) []bindingRow {
	states := b.NamespaceStates()
	if len(states) == 0 {
		return []bindingRow{{binding: b}}
	}
	rows := make([]bindingRow, 0, len(states))
	for _, ns := range states {
		rows = append(rows, bindingRow{binding: b, namespace: ns})
	}
	return rows
}

// phaseRemoving is the PHASE of a namespace removed from a binding whose
// Service and Secret are still being withdrawn.
const phaseRemoving = "Removing"

// bindingNamespacePhase is a namespace's phase: Pending until the platform
// reports it, Removing while it is withdrawn.
func bindingNamespacePhase(ns client.BindingNamespace) string {
	switch {
	case ns.Name == "":
		return ""
	case !ns.Listed:
		return phaseRemoving
	case !ns.Reported || ns.Status.Phase == "":
		return client.PhasePending
	}
	return ns.Status.Phase
}

// databaseColumns are the list columns for `kupe database list`: VERSION
// is the requested major, RUNNING the full version the platform reports.
func databaseColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return database(v).Name }},
		{Name: "ENGINE", Get: func(v any) string { return database(v).Engine }},
		{Name: "VERSION", Get: func(v any) string { return database(v).Version }},
		{Name: "RUNNING", Get: func(v any) string { return databaseStatus(database(v)).CurrentVersion }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(databaseStatus(database(v)).Phase, colorEnabled)
		}},
		{Name: "HA", Get: func(v any) string { return onOff(database(v).HighAvailability) }},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(database(v).CreatedAt) }},
		{Name: "CPU", Wide: true, Get: func(v any) string { return databaseResources(database(v)).CPU }},
		{Name: "MEMORY", Wide: true, Get: func(v any) string { return databaseResources(database(v)).Memory }},
		{Name: "STORAGE", Wide: true, Get: func(v any) string { return databaseResources(database(v)).Storage }},
		{Name: "INSTANCES", Wide: true, Get: func(v any) string { return databaseInstances(database(v)) }},
		{Name: "PROTECTED", Wide: true, Get: func(v any) string { return onOff(database(v).DeletionProtection) }},
		{Name: "BACKUPS", Wide: true, Get: func(v any) string { return backupColumn(database(v)) }},
	}
}

// databaseDetailColumns is the key:value layout for `kupe database get`.
func databaseDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return database(v).Name }},
		{Name: "Engine", Get: func(v any) string { return database(v).Engine }},
		{Name: "Version", Get: func(v any) string { return databaseVersionDisplay(database(v)) }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(databaseStatus(database(v)).Phase, colorEnabled)
		}},
		{Name: "High Availability", Get: func(v any) string { return onOff(database(v).HighAvailability) }},
		{Name: "Instances", Get: func(v any) string { return databaseInstances(database(v)) }},
		{Name: "CPU", Get: func(v any) string { return databaseResources(database(v)).CPU }},
		{Name: "Memory", Get: func(v any) string { return databaseResources(database(v)).Memory }},
		{Name: "Storage", Get: func(v any) string { return databaseResources(database(v)).Storage }},
		{Name: "Deletion Protection", Get: func(v any) string { return onOff(database(v).DeletionProtection) }},
		{Name: "Backups", Get: func(v any) string { return backupSettings(database(v)) }},
		{Name: "Last Backup", Get: func(v any) string { return lastBackup(database(v)) }},
		{Name: "Recovery Window", Get: func(v any) string { return recoveryWindow(database(v)) }},
		{Name: "Backup Size", Get: func(v any) string { return backupSize(database(v)) }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(database(v).CreatedAt) }},
	}
}

// databaseBindingColumns are the list columns for `kupe database binding
// list`: one row per namespace of each binding.
func databaseBindingColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return bindingRowOf(v).binding.Name }},
		{Name: "CLUSTER", Get: func(v any) string { return bindingRowOf(v).binding.Cluster }},
		{Name: "NAMESPACE", Get: func(v any) string { return bindingRowOf(v).namespace.Name }},
		{Name: "ROLE", Get: func(v any) string { return bindingRowOf(v).binding.Role }},
		{Name: "SECRET", Get: func(v any) string { return bindingRowOf(v).binding.EffectiveSecretName() }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(bindingNamespacePhase(bindingRowOf(v).namespace), colorEnabled)
		}},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(bindingRowOf(v).binding.CreatedAt) }},
		{Name: "HOST", Wide: true, Get: func(v any) string { return bindingRowOf(v).namespace.Status.Host }},
		{Name: "MESSAGE", Wide: true, Get: func(v any) string { return bindingRowOf(v).namespace.Status.Message }},
	}
}

// bindingNamespaceColumns is the namespace table under a binding's detail
// view.
func bindingNamespaceColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAMESPACE", Get: func(v any) string { return bindingRowOf(v).namespace.Name }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(bindingNamespacePhase(bindingRowOf(v).namespace), colorEnabled)
		}},
		{Name: "HOST", Get: func(v any) string { return bindingRowOf(v).namespace.Status.Host }},
		{Name: "MESSAGE", Get: func(v any) string { return bindingRowOf(v).namespace.Status.Message }},
	}
}

// databaseBindingDetailColumns is the key:value layout for
// `kupe database binding get` and `add`, above the namespace table.
func databaseBindingDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return databaseBinding(v).Name }},
		{Name: "Database", Get: func(v any) string { return databaseBinding(v).Database }},
		{Name: "Cluster", Get: func(v any) string { return databaseBinding(v).Cluster }},
		{Name: "Namespaces", Get: func(v any) string { return strings.Join(databaseBinding(v).Namespaces, ", ") }},
		{Name: "Role", Get: func(v any) string { return databaseBinding(v).Role }},
		{Name: "Secret", Get: func(v any) string { return databaseBinding(v).EffectiveSecretName() }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(bindingStatus(databaseBinding(v)).Phase, colorEnabled)
		}},
		{Name: "Message", Get: func(v any) string { return bindingStatus(databaseBinding(v)).Message }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(databaseBinding(v).CreatedAt) }},
	}
}

func database(v any) *client.Database { return rowOf[client.Database](v) }

func databaseBinding(v any) *client.DatabaseBinding { return rowOf[client.DatabaseBinding](v) }

// bindingRowOf asserts a table row into a bindingRow with a binding, never
// nil.
func bindingRowOf(v any) bindingRow {
	row := *rowOf[bindingRow](v)
	if row.binding == nil {
		row.binding = new(client.DatabaseBinding)
	}
	return row
}

func databaseStatus(db *client.Database) client.DatabaseStatus { return valueOf(db.Status) }

func databaseResources(db *client.Database) client.DatabaseResources { return valueOf(db.Resources) }

func bindingStatus(b *client.DatabaseBinding) client.DatabaseBindingStatus { return valueOf(b.Status) }

// databaseVersionDisplay is the detail view's Version: the requested
// major, the running version and, mid-upgrade, the one being upgraded from
// (versionDisplay): "18 (running 18.0)".
func databaseVersionDisplay(db *client.Database) string {
	st := databaseStatus(db)
	return versionDisplay(db.Version, st.CurrentVersion, st.PreviousVersion)
}

// databaseInstances renders "ready/desired", empty before the platform
// has reported either.
func databaseInstances(db *client.Database) string {
	st := databaseStatus(db)
	if st.InstancesDesired == 0 && st.InstancesReady == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", st.InstancesReady, st.InstancesDesired)
}

// backupRetention renders the retention as "14 days", empty while the
// database has not set one (the platform default applies).
func backupRetention(db *client.Database) string {
	if db.Backup == nil || db.Backup.Retention == nil {
		return ""
	}
	return fmt.Sprintf("%d days", *db.Backup.Retention)
}

// backupColumn is the wide list's BACKUPS cell: the retention, or
// "defaults" for a database that never set its backup settings
// (backup: null), as the Backups row of kupe database get does.
func backupColumn(db *client.Database) string {
	if db.Backup == nil {
		return "defaults"
	}
	return backupRetention(db)
}

// backupSettings renders "14 days, Daily": the retention and the
// schedule, each left out while unset. "defaults" for a database that
// never set its backup settings (backup: null), which keeps the platform
// defaults.
func backupSettings(db *client.Database) string {
	if db.Backup == nil {
		return "defaults"
	}
	parts := make([]string, 0, 2)
	if r := backupRetention(db); r != "" {
		parts = append(parts, r)
	}
	if db.Backup.Schedule != "" {
		parts = append(parts, db.Backup.Schedule)
	}
	return strings.Join(parts, ", ")
}

// lastBackup renders when the newest full backup finished, with its age;
// "never" until the first one completes.
func lastBackup(db *client.Database) string {
	if at := databaseStatus(db).LastBackupAt; at != "" {
		return createdWithAge(at)
	}
	return "never"
}

// recoveryWindow renders "EARLIEST to LATEST", empty until the backups
// report a window.
func recoveryWindow(db *client.Database) string {
	w := databaseStatus(db).RecoveryWindow
	if w == nil {
		return ""
	}
	return w.Earliest + " to " + w.Latest
}

// backupSize renders the backups' storage in binary units, empty until
// the platform has measured it (0).
func backupSize(db *client.Database) string {
	if n := databaseStatus(db).BackupSizeBytes; n > 0 {
		return humanBytes(n)
	}
	return ""
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// createdWithAge renders "2026-10-09T10:00:00Z (3d ago)", or the raw
// timestamp when it can't be parsed.
func createdWithAge(ts string) string {
	if age := ageFromRFC3339(ts); age != "" {
		return ts + " (" + age + " ago)"
	}
	return ts
}
