package database

import (
	"fmt"

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

// translateUpdateErr adds the refusal only a resize can get: a smaller
// storage size.
func translateUpdateErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseCodeStorageShrinkUnsupported {
		return cli.WrapAPI(cli.ExitMisuse, "database storage can grow but never shrink", err).
			WithHint(fmt.Sprintf("pass a --storage at least the current size, shown by:  kupe db get %s", name))
	}
	return translateErr(err, name)
}

// translateDeleteErr adds the delete-specific refusals: deletion protection
// and a database that kept changing while the delete was in flight (a 409
// with no code, after kupe-api's own retries).
func translateDeleteErr(err error, name string) error {
	switch {
	case client.ErrorCode(err) == client.DatabaseDeleteCodeProtected:
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("database %q has deletion protection on", name), err).
			WithHint(fmt.Sprintf("turn it off first:  kupe db update %s --deletion-protection=false", name))
	case client.IsConflict(err) && client.ErrorCode(err) == "":
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("database %q changed while it was being deleted", name), err).
			WithHint(fmt.Sprintf("retry:  kupe db delete %s", name))
	}
	return translateErr(err, name)
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
