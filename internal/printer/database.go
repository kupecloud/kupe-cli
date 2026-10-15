package printer

import (
	"fmt"

	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// Database renders databases.
var Database = Resource[client.Database]{
	Columns:       databaseColumns,
	DetailColumns: databaseDetailColumns,
	Name:          func(db *client.Database) string { return db.Name },
}

// DatabaseBinding renders database bindings.
var DatabaseBinding = Resource[client.DatabaseBinding]{
	Columns:       databaseBindingColumns,
	DetailColumns: databaseBindingDetailColumns,
	Name:          func(b *client.DatabaseBinding) string { return b.Name },
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
		{Name: "Created", Get: func(v any) string { return createdWithAge(database(v).CreatedAt) }},
	}
}

// databaseBindingColumns are the list columns for `kupe database binding list`.
func databaseBindingColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return databaseBinding(v).Name }},
		{Name: "CLUSTER", Get: func(v any) string { return databaseBinding(v).Cluster }},
		{Name: "NAMESPACE", Get: func(v any) string { return databaseBinding(v).Namespace }},
		{Name: "ROLE", Get: func(v any) string { return databaseBinding(v).Role }},
		{Name: "SECRET", Get: func(v any) string { return databaseBinding(v).EffectiveSecretName() }},
		{Name: "PHASE", Get: func(v any) string {
			return ux.ColorPhase(bindingStatus(databaseBinding(v)).Phase, colorEnabled)
		}},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(databaseBinding(v).CreatedAt) }},
		{Name: "HOST", Wide: true, Get: func(v any) string { return bindingStatus(databaseBinding(v)).Host }},
		{Name: "MESSAGE", Wide: true, Get: func(v any) string { return bindingStatus(databaseBinding(v)).Message }},
	}
}

// databaseBindingDetailColumns is the key:value layout for
// `kupe database binding get` and `add`.
func databaseBindingDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return databaseBinding(v).Name }},
		{Name: "Database", Get: func(v any) string { return databaseBinding(v).Database }},
		{Name: "Cluster", Get: func(v any) string { return databaseBinding(v).Cluster }},
		{Name: "Namespace", Get: func(v any) string { return databaseBinding(v).Namespace }},
		{Name: "Role", Get: func(v any) string { return databaseBinding(v).Role }},
		{Name: "Secret", Get: func(v any) string { return databaseBinding(v).EffectiveSecretName() }},
		{Name: "Host", Get: func(v any) string { return bindingStatus(databaseBinding(v)).Host }},
		{Name: "Phase", Get: func(v any) string {
			return ux.ColorPhase(bindingStatus(databaseBinding(v)).Phase, colorEnabled)
		}},
		{Name: "Message", Get: func(v any) string { return bindingStatus(databaseBinding(v)).Message }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(databaseBinding(v).CreatedAt) }},
	}
}

func database(v any) *client.Database { return rowOf[client.Database](v) }

func databaseBinding(v any) *client.DatabaseBinding { return rowOf[client.DatabaseBinding](v) }

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
