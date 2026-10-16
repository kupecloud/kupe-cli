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

// translateCreateErr adds the refusal only a create can get: a name longer
// than the tenant may use. It exits 2 like any other refused name.
func translateCreateErr(err error, name string) error {
	if client.ErrorCode(err) == client.DatabaseCodeNameTooLong {
		return cli.WrapAPI(cli.ExitMisuse, fmt.Sprintf("database name %q is too long for this tenant", name), err).
			WithHint("choose a shorter name: database names may be at most 44 characters, and fewer when the tenant name is over 12")
	}
	return translateErr(err, name)
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
