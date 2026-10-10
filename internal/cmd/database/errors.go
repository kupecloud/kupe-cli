package database

import (
	"fmt"

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
			WithHint("this is a platform setting, not an outage, so retrying will not help; " +
				"existing databases can still be listed, unprotected (--deletion-protection=false) and deleted")
	case client.IsCreationPaused(err):
		return cli.Wrap(cli.ExitUnavailable, "database creation is paused", err).
			WithHint("retry later; status updates are posted at https://status.kupe.cloud")
	}
	return err
}

// translateUpdateErr adds the refusal only a resize can get: a smaller
// storage size.
func translateUpdateErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseCodeStorageShrinkUnsupported {
		return cli.Wrap(cli.ExitMisuse, "database storage can grow but never shrink", err).
			WithHint(fmt.Sprintf("pass a --storage at least the current size, shown by:  kupe db get %s", name))
	}
	return translateErr(err)
}

// translateDeleteErr adds the delete-specific refusals: deletion protection
// and a database that kept changing while the delete was in flight.
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

// translateUpgradeErr shows the platform's reason when it does not offer
// the requested major upgrade.
func translateUpgradeErr(err error) error {
	if client.ErrorCode(err) == client.DatabaseCodeMajorUpgradeUnavailable {
		return cli.Wrap(cli.ExitUnavailable, "the platform refused the major version upgrade", err)
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
