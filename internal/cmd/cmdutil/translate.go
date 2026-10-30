package cmdutil

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// refusal is one refusal every managed service can get. kupe-api names it
// with code; servers from before the codes send only the operator's
// sentence, which phrase (lower-case) matches.
type refusal struct {
	code   string
	phrase string
	exit   int
	// fallback is the CLI's sentence, shown only when the API sent no
	// message (cli.WrapAPI). hint, when set, is the next step.
	fallback func(noun, name string) string
	hint     func(noun, name string) string
	// hintStatus, when set, gives the hint only for a refusal with this
	// HTTP status: the same code can mean different things at another one.
	hintStatus int
}

// refusals is tried in order; the first row whose code, or failing that
// whose phrase, matches wins.
var refusals = []refusal{
	{
		code: client.CodeCreationPaused, phrase: "creation is temporarily paused", exit: cli.ExitUnavailable,
		fallback: func(noun, _ string) string { return noun + " creation is paused" },
		hint: func(string, string) string {
			return "retry later; status updates are posted at https://status.kupe.cloud"
		},
	},
	{
		code: client.DatabaseCodeUnavailable, phrase: "managed databases are not enabled on this platform", exit: cli.ExitNotFound,
		fallback: func(string, string) string { return "databases are not available on this platform" },
		hint: func(string, string) string {
			return "this is a platform setting, not an outage, so retrying will not help; " +
				"existing databases can still be listed, unprotected (--deletion-protection=false) and deleted, " +
				"and their bindings deleted or narrowed (binding update --remove-namespace); " +
				"manual backups can still be listed and deleted (kupe database backup delete)"
		},
	},
	{
		code: client.RegistryCodeUnavailable, phrase: "registries are not enabled on this platform", exit: cli.ExitNotFound,
		fallback: func(string, string) string { return "registries are not available on this platform" },
		hint: func(string, string) string {
			return "this is a platform setting, not an outage, so retrying will not help; " +
				"an existing registry can still be listed, unprotected (--deletion-protection=false) and deleted, " +
				"and its cluster connections removed (kupe registry disconnect)"
		},
	},
	{
		// The platform does not offer backups: a request that sets backup
		// settings, takes a manual backup or restores is refused. The
		// request was valid, so not a misuse. Only the 400 (backups
		// switched off) leaves manual backups listable; the 404 means the
		// platform cannot take them at all (callers word it themselves).
		code: client.DatabaseCodeBackupsUnavailable, phrase: "backups are not available on this platform", exit: cli.ExitUnavailable,
		fallback: func(string, string) string { return "backups are not available on this platform" },
		hint: func(string, string) string {
			return "this is a platform setting, not an outage, so retrying will not help; " +
				"existing manual backups can still be listed and deleted (kupe database backup delete)"
		},
		hintStatus: http.StatusBadRequest,
	},
	{
		code: client.CodeTerminating, phrase: "is being deleted; it cannot be bound", exit: cli.ExitConflict,
		fallback: func(noun, name string) string { return fmt.Sprintf("%s %q is being deleted", noun, name) },
		hint: func(noun, name string) string {
			if name == "" {
				return ""
			}
			return fmt.Sprintf("it can no longer be changed; to wait until it is gone:  kupe %s wait %s --for deleted", noun, name)
		},
	},
	{
		code: client.CodeTenantTerminating, phrase: "is being deleted; cannot ", exit: cli.ExitConflict,
		fallback: func(noun, _ string) string {
			return "your tenant is being deleted; it cannot take new or larger " + plural(noun)
		},
	},
	{
		code: client.CodeTenantSuspended, phrase: "is suspended; contact support", exit: cli.ExitConflict,
		fallback: func(noun, _ string) string {
			return "your tenant is suspended; it cannot take new or larger " + plural(noun)
		},
		hint: func(string, string) string { return "contact support to lift the suspension" },
	},
	{
		code: client.CodePaymentSetupRequired, phrase: "is awaiting payment setup; ", exit: cli.ExitConflict,
		fallback: func(noun, _ string) string { return "add a payment method before creating or growing " + plural(noun) },
		hint: func(string, string) string {
			return "add a payment method on the Billing page of the Kupe console, then retry"
		},
	},
	{
		code: client.CodePlanLimitReached, phrase: " limit reached: ", exit: cli.ExitConflict,
		fallback: func(noun, _ string) string { return "your plan's " + noun + " limit is reached" },
		hint: func(noun, _ string) string {
			return fmt.Sprintf("delete a %s (\"kupe %s delete <name>\") or upgrade your plan", noun, noun)
		},
	},
}

// TranslateCommon maps the refusals every managed service shares onto CLI
// errors: the exit code by refusal class, the API's own message as the text
// (the CLI's sentence only when the API sent none), and a next step. kupe-api's
// code decides; the operator's sentence is matched only for servers that
// predate the codes. A 503 nothing else matched gets a retry hint: writes
// are never retried automatically, since a create may not be repeated
// blindly. noun is the command noun ("cluster", "database"); name is the
// resource the command acted on, "" when none. Anything unrecognised is
// returned unchanged, so a service translates its own refusals first and
// passes the rest here.
func TranslateCommon(noun, name string, err error) error {
	if err == nil {
		return nil
	}
	if r, ok := matchRefusal(err); ok {
		out := cli.WrapAPI(r.exit, r.fallback(noun, name), err)
		if r.hint != nil && (r.hintStatus == 0 || client.StatusCode(err) == r.hintStatus) {
			if hint := r.hint(noun, name); hint != "" {
				out = out.WithHint(hint)
			}
		}
		return out
	}
	if client.IsUnavailable(err) {
		return cli.WrapAPI(cli.ExitUnavailable, "kupe-api is temporarily unavailable", err).
			WithHint("retry in a few seconds; status updates are posted at https://status.kupe.cloud")
	}
	return err
}

// matchRefusal finds err's row in refusals: by code, or for a server that
// sent no code, by the operator's sentence.
func matchRefusal(err error) (refusal, bool) {
	if !client.IsAPIError(err) {
		return refusal{}, false
	}
	if code := client.ErrorCode(err); code != "" {
		for _, r := range refusals {
			if r.code == code {
				return r, true
			}
		}
		return refusal{}, false
	}
	msg := strings.ToLower(err.Error())
	for _, r := range refusals {
		if strings.Contains(msg, r.phrase) {
			return r, true
		}
	}
	return refusal{}, false
}

// plural is noun's plural for the fallback sentences: "registries",
// "databases".
func plural(noun string) string {
	if strings.HasSuffix(noun, "y") {
		return strings.TrimSuffix(noun, "y") + "ies"
	}
	return noun + "s"
}
