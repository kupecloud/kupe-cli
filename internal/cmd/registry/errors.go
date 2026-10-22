package registry

import (
	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
)

// createHint is the next step when the tenant has no registry.
const createHint = "this tenant has no registry; create one:  kupe registry create"

// unprotectHint is the next step for a delete refused by deletion
// protection.
const unprotectHint = "turn deletion protection off first:  kupe registry update --deletion-protection=false"

// refusal is the CLI's answer to one registry refusal code: the exit code,
// the CLI's own sentence for when the API sent no message, and a hint.
type refusal struct {
	exit     int
	fallback string
	hint     func(cluster string) string
}

func fixed(hint string) func(string) string { return func(string) string { return hint } }

// refusals maps the registry's codes (lower-case, as client.ErrorCode
// returns them). The API's message is always the error text when it sent
// one.
var refusals = map[string]refusal{
	client.RegistryCodeDeletionProtected: {cli.ExitConflict, "the registry has deletion protection on", fixed(unprotectHint)},
	client.RegistryCodeSessionRequiresUser: {cli.ExitAuth, "login sessions need a person signed in", fixed(
		"sign in with:  kupe auth login\nfor automation, use an access token:  kupe registry token create --name NAME --access pull|push")},
	client.RegistryCodeAlreadyExists: {cli.ExitConflict, "this tenant already has a registry", fixed("see it:  kupe registry get")},
	client.RegistryCodeBindingExists: {cli.ExitConflict, "the cluster is already connected", func(cluster string) string {
		return "see the connection:  kupe registry connection get " + orPlaceholder(cluster)
	}},
	client.RegistryCodePushNamespacesLimit:   {cli.ExitMisuse, "a cluster connection can have at most 10 push namespaces", nil},
	client.RegistryCodeStorageLimitAbovePlan: {cli.ExitMisuse, "the storage limit is above your plan's", nil},
	client.RegistryCodeTokenExpiryInvalid:    {cli.ExitMisuse, "the expiry is out of range", nil},
	client.RegistryCodePushNamespacesServiceLimit: {cli.ExitConflict, "the registry has too many push namespaces", fixed(
		"a registry gives push credentials to at most 20 namespaces across all its connections; " +
			"remove one from another connection (kupe registry connection list)")},
	client.RegistryCodeTokenPushAdminOnly: {cli.ExitAuth, "only tenant admins can create push access tokens", fixed(
		"create a pull token instead:  --access pull")},
	client.RegistryCodeTokenLimitReached: {cli.ExitConflict, "the registry has as many access tokens as it can hold", fixed(
		"revoke one first; to list them:  kupe registry token list --kind token")},
	client.RegistryCodeImageInUse: {cli.ExitConflict, "the image is part of a multi-platform image", fixed(
		"delete the multi-platform image that contains it")},
	client.RegistryCodeImageProtected: {cli.ExitConflict, "the registry refused to delete it", nil},
	client.RegistryCodeNotReady: {cli.ExitConflict, "the registry is not ready yet", fixed(
		"wait until it is:  kupe registry wait --for ready")},
	client.CodeTerminating: {cli.ExitConflict, "it is being deleted", func(cluster string) string {
		if cluster != "" {
			return "check:  kupe registry connection get " + cluster
		}
		return "check:  kupe registry get"
	}},
	client.CodePlanLimitReached: {cli.ExitConflict, "your plan does not include a registry", fixed(
		"upgrade to a plan that includes a registry")},
}

func orPlaceholder(cluster string) string {
	if cluster == "" {
		return "CLUSTER"
	}
	return cluster
}

// translateErr maps a registry refusal onto a CLI error: the exit code by
// refusal class, the API's message as the text and a next step. cluster is
// the connection the command acted on, "" when none. A 404 that is the
// registry itself missing says how to create one; any other 404 keeps the
// API's message, which names the repository, image, tag, connection or
// token. What the registry does not refuse itself falls to the refusals
// every service shares (cmdutil.TranslateCommon).
func translateErr(err error, cluster string) error {
	if err == nil {
		return nil
	}
	if client.IsRegistryMissing(err) {
		return noRegistry(err)
	}
	if r, ok := refusals[client.ErrorCode(err)]; ok {
		out := cli.WrapAPI(r.exit, r.fallback, err)
		if r.hint != nil {
			out = out.WithHint(r.hint(cluster))
		}
		return out
	}
	return cmdutil.TranslateCommon(noun, "", err)
}

// translateRegistryErr is translateErr for a command on the registry
// itself, where any 404 without a code means it does not exist (also the
// answer of a kupe-api that predates registries).
func translateRegistryErr(err error) error {
	if client.IsNotFound(err) && client.ErrorCode(err) == "" {
		return noRegistry(err)
	}
	return translateErr(err, "")
}

// noRegistry is exit 4 with the API's message and how to create a registry.
func noRegistry(err error) error {
	return cli.WrapAPI(cli.ExitNotFound, "this tenant has no registry", err).WithHint(createHint)
}

// translateDeleteErr adds what deleting the registry can be refused for to
// the refusals every delete can get (cmdutil.Service.TranslateDelete).
func translateDeleteErr(err error, name string) error {
	if client.ErrorCode(err) == client.RegistryCodeDeletionProtected {
		return translateErr(err, "")
	}
	return service.TranslateDelete(err, name)
}
