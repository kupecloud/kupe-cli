package database

import (
	"errors"
	"fmt"
	"net/http"
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
	// A pre-access API rejects only these new fields before interpreting them.
	// Keep its refusal and request ID; never retry after dropping settings.
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == "" {
		for _, field := range []string{"postgres", "roleName", "credentials"} {
			if strings.HasSuffix(strings.TrimSpace(apiErr.Message), `json: unknown field "`+field+`"`) {
				return cli.WrapAPI(cli.ExitUnavailable, "this platform does not support this database setting yet", err).
					WithHint("this platform does not support this database setting yet")
			}
		}
	}

	switch client.ErrorCode(err) {
	case client.DatabaseCodeDedicatedCredentialsIrreversible:
		return dedicatedIrreversible(err)
	case client.DatabaseCodeRoleReserved:
		return cli.WrapAPI(cli.ExitMisuse, "that role name is reserved", err).WithHint("choose another declared role name")
	case client.DatabaseCodeExtensionNotAllowed:
		return cli.WrapAPI(cli.ExitMisuse, "that extension is not available", err).WithHint("choose an extension from the platform's allowlist in the error")
	case client.DatabaseCodeRoleInUse:
		return cli.WrapAPI(cli.ExitConflict, "the role is still in use", err).WithHint("remove its bindings before removing the declared role")
	case client.DatabaseCodeRoleNotDeclared:
		return cli.WrapAPI(cli.ExitMisuse, "the role is not declared on the database", err).WithHint("declare it with kupe database update --add-role")
	case client.DatabaseCodeCredentialsNotDedicated:
		return cli.WrapAPI(cli.ExitConflict, "the binding does not use dedicated credentials", err).WithHint("turn them on with kupe database binding update DATABASE BINDING --dedicated, or rotate the shared role with kupe database rotate-credentials")
	case client.DatabaseCodeInvalidBindingCredentials:
		return cli.WrapAPI(cli.ExitMisuse, "invalid binding credentials", err).WithHint("owner uses shared credentials; declared roles require dedicated credentials")
	}
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
		return sourceNotFound(err, source)
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
	// A kupe-api that predates restores refuses the unknown field; one
	// whose platform cannot restore yet answers 404 backups_unavailable.
	if client.IsValidation(err) && client.ErrorCode(err) == "" && strings.Contains(err.Error(), `unknown field "source"`) ||
		backupsMissing(err) {
		return noRestoresYet()
	}
	return nil
}

// noRestoresYet is noBackupsYet for a restore.
func noRestoresYet() error {
	return cli.New(cli.ExitUnavailable, "this platform does not offer database restores yet")
}

// sourceNotFound is a restore source that does not exist, whether the API
// refused the restore (restore_source_not_found) or the CLI's read of the
// source's storage got a 404: exit 2 either way, with the API's message.
func sourceNotFound(err error, source *client.DatabaseSource) error {
	hint := "list your databases:  kupe database list"
	if source.Backup != "" {
		hint = "list your manual backups:  kupe database backup list"
	}
	return cli.WrapAPI(cli.ExitMisuse, "the restore source was not found", err).WithHint(hint)
}

// translateUpdateErr adds the refusals only an update can get: a smaller
// storage size, and any change while a major upgrade runs.
func translateUpdateErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseCodeStorageShrinkUnsupported {
		return cli.WrapAPI(cli.ExitMisuse, "database storage can grow but never shrink", err).
			WithHint(fmt.Sprintf("pass a --storage at least the current size, shown by:  kupe db get %s", name))
	}
	// A resize, an HA change or a settings change while an upgrade runs.
	if client.ErrorCode(err) == client.DatabaseCodeUpgradeInProgress {
		return translateUpgradeRefusal(err, name)
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
	if out := translateUpgradeRefusal(err, name); out != nil {
		return out
	}
	return translateErr(err, name)
}

// translateUpgradeRefusal maps the two conflicts a version change can get
// beyond the shared ones, or returns nil for any other error. The platform's
// message says what is wrong and is kept as it is.
func translateUpgradeRefusal(err error, name string) error {
	switch client.ErrorCode(err) {
	case client.DatabaseCodeUpgradeNotReady:
		return cli.WrapAPI(cli.ExitConflict, "the database is not ready to upgrade", err).
			WithHint(fmt.Sprintf("the message above says what is missing; check the database:  kupe db get %s", name))
	case client.DatabaseCodeUpgradeInProgress:
		return cli.WrapAPI(cli.ExitConflict, "a major upgrade of the database is already running", err).
			WithHint(fmt.Sprintf("wait for it to finish:  kupe db wait %s", name))
	}
	return nil
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
	if backupsMissing(err) {
		return noBackupsYet()
	}
	return translateErr(err, "")
}

// backupsMissing reports whether err says the platform cannot take manual
// backups at all: a kupe-api that predates them (no route), or one whose
// platform has no backup kind installed (404 backups_unavailable). The
// 400 backups_unavailable (backups switched off) is not this: manual
// backups can still be listed and deleted then.
func backupsMissing(err error) bool {
	return client.IsUnknownRoute(err) ||
		(client.IsNotFound(err) && client.ErrorCode(err) == client.DatabaseCodeBackupsUnavailable)
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
	if backupsMissing(err) {
		return noBackupsYet()
	}
	return translateErr(err, database)
}

// An older API must never trigger a retry with requested settings removed.
func logicalFeatureErr(err error, requested bool) error {
	var apiErr *client.APIError
	if requested && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == "" {
		for _, field := range []string{"databases", "logicalDatabase", "postgres"} {
			if strings.HasSuffix(strings.TrimSpace(apiErr.Message), `json: unknown field "`+field+`"`) {
				return cli.WrapAPI(cli.ExitUnavailable, "the platform does not offer logical databases yet", err).WithHint("the platform does not offer logical databases yet")
			}
		}
	}
	return err
}
