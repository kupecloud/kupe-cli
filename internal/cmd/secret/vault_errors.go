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
			return cli.Wrap(cli.ExitConflict, path+" changed after it was read; run purge again", err)
		}
		return cli.Wrap(cli.ExitConflict, fmt.Sprintf("the values at %s changed since version %d", path, version), err).WithHint("`kupe secret values get` shows the current version; review it before retrying with that version")
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
		return cli.Wrap(cli.ExitConflict, path+" changed after it was read; run purge again", err)
	}
	return err
}
