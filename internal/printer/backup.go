package printer

import (
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

// Backup renders manual database backups.
var Backup = Resource[client.Backup]{
	Columns:       backupColumns,
	DetailColumns: backupDetailColumns,
	Name:          func(b *client.Backup) string { return b.Name },
}

// deletedMarker follows the name of a database that was deleted.
const deletedMarker = " (deleted)"

// backupColumns are the list columns for `kupe database backup list`.
func backupColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return backup(v).Name }},
		{Name: "DATABASE", Get: func(v any) string { return backupDatabase(backup(v)) }},
		{Name: "REASON", Get: func(v any) string { return backup(v).Reason }},
		{Name: "PHASE", Get: func(v any) string { return ux.ColorPhase(backupStatus(backup(v)).Phase, colorEnabled) }},
		{Name: "SIZE", Get: func(v any) string { return backupBytes(backup(v)) }},
		{Name: "COMPLETED", Get: func(v any) string { return ageFromRFC3339(backupStatus(backup(v)).CompletedAt) }},
		{Name: "AGE", Get: func(v any) string { return ageFromRFC3339(backup(v).CreatedAt) }},
	}
}

// backupDetailColumns is the key:value layout for `kupe database backup
// get` and `create`. Version and Storage are the database's when the
// backup was taken: a database restored from it runs that version and
// needs at least that storage.
func backupDetailColumns(colorEnabled bool) Columns {
	return Columns{
		{Name: "Name", Get: func(v any) string { return backup(v).Name }},
		{Name: "Database", Get: func(v any) string { return backupDatabase(backup(v)) }},
		{Name: "Reason", Get: func(v any) string { return backup(v).Reason }},
		{Name: "Phase", Get: func(v any) string { return ux.ColorPhase(backupStatus(backup(v)).Phase, colorEnabled) }},
		{Name: "Message", Get: func(v any) string { return backup(v).Status.Message() }},
		{Name: "Size", Get: func(v any) string { return backupBytes(backup(v)) }},
		{Name: "Version", Get: func(v any) string { return backupSource(backup(v)).Version }},
		{Name: "Storage", Get: func(v any) string { return backupSource(backup(v)).Storage }},
		{Name: "Completed", Get: func(v any) string { return createdWithAge(backupStatus(backup(v)).CompletedAt) }},
		{Name: "Created", Get: func(v any) string { return createdWithAge(backup(v).CreatedAt) }},
	}
}

func backup(v any) *client.Backup { return rowOf[client.Backup](v) }

func backupStatus(b *client.Backup) client.BackupStatus { return valueOf(b.Status) }

func backupSource(b *client.Backup) client.BackupSource { return valueOf(backupStatus(b).Source) }

// backupDatabase is the database the backup was taken of, with
// deletedMarker once that database is deleted.
func backupDatabase(b *client.Backup) string {
	if backupStatus(b).DatabaseDeleted {
		return b.Database + deletedMarker
	}
	return b.Database
}

// backupBytes renders the backup's size in binary units, empty until the
// platform has measured it (0).
func backupBytes(b *client.Backup) string {
	if n := backupStatus(b).SizeBytes; n > 0 {
		return HumanBytes(n)
	}
	return ""
}
