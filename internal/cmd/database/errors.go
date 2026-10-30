package database

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

// translateErr maps the refusals every service shares (creation paused,
// databases switched off, terminating, the tenant gates, the plan limit, a
// 503) onto CLI errors with a next step; see cmdutil.TranslateCommon. name
// is the database the command acted on, "" when none. The API's own message
// is the error text; the CLI's sentence is only a fallback for a refusal
// that carries none (cli.WrapAPI). Anything unrecognised passes through
// unchanged.
func translateErr(err error, name string) error {
	return cmdutil.TranslateCommon(noun, name, err)
}

// translateCreateErr adds the refusals only a create can get: a name
// longer than the tenant may use (exit 2 like any other refused name), and
// a restore's (source non-nil): a source that does not fit is a misuse
// (exit 2), one not ready yet a conflict (exit 5).
func translateCreateErr(err error, name string, source *client.DatabaseSource) error {
	if client.ErrorCode(err) == client.DatabaseCodeNameTooLong {
		return cli.WrapAPI(cli.ExitMisuse, fmt.Sprintf("database name %q is too long for this tenant", name), err).
			WithHint("choose a shorter name: database names may be at most 44 characters, and fewer when the tenant name is over 12")
	}
	if source != nil {
		if out := translateRestoreErr(err, source); out != nil {
			return out
		}
	}
	return translateErr(err, name)
}

// translateRestoreErr maps the refusals of a restore from source, or
// returns nil for any other error.
func translateRestoreErr(err error, source *client.DatabaseSource) error {
	check := "check it:  kupe database get " + source.Database
	if source.Backup != "" {
		check = "check it:  kupe database backup get " + source.Backup
	}
	switch client.ErrorCode(err) {
	case client.RestoreCodeSourceNotFound:
		hint := "list your databases:  kupe database list"
		if source.Backup != "" {
			hint = "list your manual backups:  kupe database backup list"
		}
		return cli.WrapAPI(cli.ExitMisuse, "the restore source was not found", err).WithHint(hint)
	case client.RestoreCodeSourceNotReady:
		hint := "a database can be restored once its first backup has completed and it has a restore window\n" + check
		if source.Backup != "" {
			hint = "a backup can be restored once it is Completed\n" + check
		}
		return cli.WrapAPI(cli.ExitConflict, "the restore source is not ready yet", err).WithHint(hint)
	case client.RestoreCodePointOutsideWindow:
		return cli.WrapAPI(cli.ExitMisuse, "--point-in-time is outside the database's restore window", err).
			WithHint("pick a moment inside its Restore Window, or leave out --point-in-time for the latest point\n" + check)
	case client.RestoreCodeVersionMismatch:
		return cli.WrapAPI(cli.ExitMisuse, "a restore runs the source's major version", err).
			WithHint("leave out --version, or give the source's\n" + check)
	case client.RestoreCodeStorageTooSmall:
		return cli.WrapAPI(cli.ExitMisuse, "--storage is less than the source's", err).
			WithHint("leave out --storage to take the source's, or give at least that much\n" + check)
	}
	// A kupe-api that predates restores refuses the unknown field.
	if client.IsValidation(err) && client.ErrorCode(err) == "" && strings.Contains(err.Error(), `unknown field "source"`) {
		return cli.New(cli.ExitUnavailable, "this platform does not offer database restores yet")
	}
	return nil
}

// translateUpdateErr adds the refusal only a resize can get: a smaller
// storage size.
func translateUpdateErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseCodeStorageShrinkUnsupported {
		return cli.WrapAPI(cli.ExitMisuse, "database storage can grow but never shrink", err).
			WithHint(fmt.Sprintf("pass a --storage at least the current size, shown by:  kupe db get %s", name))
	}
	return translateErr(err, name)
}

// translateDeleteErr adds deletion protection to the refusals every delete
// can get (cmdutil.Service.TranslateDelete).
func translateDeleteErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseDeleteCodeProtected {
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("database %q has deletion protection on", name), err).
			WithHint(fmt.Sprintf("turn it off first:  kupe db update %s --deletion-protection=false", name))
	}
	return service.TranslateDelete(err, name)
}

// translateUpgradeErr exits 7, not 2, when the platform does not offer the
// requested major upgrade: the request was valid, the platform declined it.
func translateUpgradeErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseCodeMajorUpgradeUnavailable {
		return cli.WrapAPI(cli.ExitUnavailable, "the platform refused the major version upgrade", err)
	}
	return translateErr(err, name)
}

// translateRotateErr: a database being deleted is the shared terminating
// refusal; any other 409 without a code (an older server's answer for one)
// says the rotation was refused. The CLI's sentence shows only if the API
// sent no message.
func translateRotateErr(err error, name string) error {
	if client.IsConflict(err) && client.ErrorCode(err) == "" {
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("cannot rotate credentials of database %q", name), err)
	}
	return translateErr(err, name)
}

// noBackupsYet is the answer of a kupe-api that predates manual backups:
// the route does not exist there. The request was valid; the platform does
// not offer it, so it exits 7 like a refusal.
func noBackupsYet() error {
	return cli.New(cli.ExitUnavailable, "this platform does not offer database backups yet")
}

// translateBackupErr maps errors of the backup commands that name a backup
// (get, delete, list, the wait). The shared refusals get no database name,
// so no hint points at a database command.
func translateBackupErr(err error) error {
	if client.IsUnknownRoute(err) {
		return noBackupsYet()
	}
	return translateErr(err, "")
}

// translateBackupCreateErr adds the refusals only a backup of database
// can get: it is not running, or it keeps the most manual backups a
// database may. Both exit 5 like any other conflict.
func translateBackupCreateErr(err error, database string) error {
	switch client.ErrorCode(err) {
	case client.BackupCodeDatabaseNotReady:
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("database %q is not running", database), err).
			WithHint(fmt.Sprintf("back it up once it is Running:  kupe database wait %s", database))
	case client.BackupCodeLimitReached:
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("database %q already has the most manual backups it can keep", database), err).
			WithHint(fmt.Sprintf("delete one you no longer need:  kupe database backup list %s\n  then:  kupe database backup delete BACKUP", database))
	}
	if client.IsUnknownRoute(err) {
		return noBackupsYet()
	}
	return translateErr(err, database)
}
