package secret

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func translateVaultErr(err error, path string, version int64, purge bool) error {
	if err == nil {
		return nil
	}
	var api *client.APIError
	if !errors.As(err, &api) {
		return err
	}
	switch client.ErrorCode(err) {
	case "secret_version_conflict":
		if purge {
			return boundedVaultError(cli.ExitConflict, path+" is no longer at the version being purged; run purge again", err)
		}
		if version == 0 {
			return boundedVaultError(cli.ExitConflict, "values already exist at "+path, err).WithHint("`kupe secret values get` shows the current version; review it, then pass it as --expected-version")
		}
		return boundedVaultError(cli.ExitConflict, fmt.Sprintf("the values at %s changed since version %d", path, version), err).WithHint("`kupe secret values get` shows the current version; review it before retrying with that version")
	case "precondition_required":
		return cli.Wrap(cli.ExitMisuse, "a version precondition is required", err)
	case "invalid_secret_values":
		return cli.Wrap(cli.ExitMisuse, "values must be a non-empty JSON object with valid keys and string values (512 KiB limit)", err)
	case "secret_write_unconfirmed":
		return cli.Wrap(cli.ExitUnavailable, "the write may or may not have been saved", err).WithHint("run `kupe secret values get` and compare the version before deciding whether to write again")
	case "secret_path_reserved":
		return cli.Wrap(cli.ExitAuth, "this path is reserved by the platform", err)
	case "vault_not_configured":
		return cli.Wrap(cli.ExitUnavailable, "Vault is not available on this platform", err)
	case "vault_unavailable":
		e := cli.Wrap(cli.ExitUnavailable, "Vault is unavailable", err)
		if purge {
			return e.WithHint("repeat the command to confirm")
		}
		return e
	case "secret_references_exist":
		return cli.WrapAPI(cli.ExitConflict, "secret definitions still use "+path, err)
	}
	if api.StatusCode == http.StatusRequestEntityTooLarge {
		return cli.Wrap(cli.ExitMisuse, "values input exceeds the 512 KiB limit", err)
	}
	if purge && api.StatusCode == http.StatusPreconditionFailed {
		return boundedVaultError(cli.ExitConflict, path+" is no longer at the version being purged; run purge again", err)
	}
	return err
}

// translateVaultWriteErr is called only after invoking a value PUT or purge.
// Reads and local/auth setup failures must not claim a write was attempted.
func translateVaultWriteErr(err error, path string, version int64, purge bool) error {
	if err == nil {
		return nil
	}
	var api *client.APIError
	code := client.ErrorCode(err)
	unknown := !errors.As(err, &api) || code == "secret_write_unconfirmed"
	if api != nil && api.StatusCode >= 500 && api.StatusCode < 600 && code != "vault_unavailable" {
		unknown = true
	}
	if unknown || purge && code == "vault_unavailable" {
		if purge {
			return boundedVaultError(cli.ExitUnavailable, "repeat the command to confirm", err)
		}
		return boundedVaultError(cli.ExitUnavailable, "the write may or may not have been saved", err).WithHint("run `kupe secret values get` and compare the version before deciding whether to write again")
	}
	return translateVaultErr(err, path, version, purge)
}

// vaultMessage retains the cause for errors.Is/As without rendering transport
// text or an ingress body, either of which may contain values or infrastructure.
type vaultMessage struct {
	message string
	cause   error
}

func (e *vaultMessage) Error() string {
	var api *client.APIError
	if errors.As(e.cause, &api) && api.RequestID != "" {
		return e.message + " (request-id: " + api.RequestID + ")"
	}
	return e.message
}

func (e *vaultMessage) Unwrap() error { return e.cause }

func boundedVaultError(code int, message string, cause error) *cli.Error {
	return cli.Wrap(code, "", &vaultMessage{message: message, cause: cause})
}
