package database

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// translateErr maps API refusals shared by every database command onto
// CLI errors with a next step. The API's own message is always kept.
// Anything unrecognised passes through unchanged.
func translateErr(err error) error {
	switch {
	case err == nil:
		return nil
	case client.ErrorCode(err) == client.DatabaseCodeUnavailable:
		return cli.Wrap(cli.ExitNotFound, "managed databases are not available on this platform", err).
			WithHint("this is a platform setting, not an outage; retrying will not help")
	case client.IsValidation(err) && strings.Contains(err.Error(), creationPausedText):
		return cli.Wrap(cli.ExitUnavailable, "database creation is paused", err).
			WithHint("retry later; status updates are posted at https://status.kupe.cloud")
	}
	return err
}

// creationPausedText identifies the 400 POST /databases returns while the
// platform pauses database creation; the API gives it no error code.
const creationPausedText = "creation is temporarily paused"

// translateDeleteErr adds the delete-specific refusals: deletion protection
// and a database that changed while the delete was in flight.
func translateDeleteErr(err error, name string) error {
	switch {
	case client.IsConflict(err) && client.ErrorCode(err) == client.DatabaseDeleteCodeProtected:
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("database %q has deletion protection on", name), err).
			WithHint(fmt.Sprintf("turn it off first:  kupe db update %s --deletion-protection=false", name))
	case client.IsConflict(err):
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("database %q changed while it was being deleted", name), err).
			WithHint(fmt.Sprintf("retry:  kupe db delete %s", name))
	}
	return translateErr(err)
}

// translateUpgradeErr says plainly when the platform does not offer major
// upgrades yet, keeping its message.
func translateUpgradeErr(err error) error {
	if client.ErrorCode(err) == client.DatabaseCodeMajorUpgradeUnavailable {
		return cli.Wrap(cli.ExitUnavailable, "major version upgrades are not available yet", err)
	}
	return translateErr(err)
}

// translateRotateErr explains the 409 a rotation gets once the database is
// being deleted.
func translateRotateErr(err error, name string) error {
	if client.IsConflict(err) {
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("cannot rotate credentials of database %q", name), err)
	}
	return translateErr(err)
}
