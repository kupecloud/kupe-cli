package secret

import (
	"context"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// reservedVaultPath matches API #91 without cleaning or unescaping the path.
func reservedVaultPath(path string) bool {
	leading := true
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
		if segment == "" {
			continue
		}
		if leading && (segment == "secret" || segment == "data" || segment == "metadata") {
			continue
		}
		if leading && segment == "encryption" {
			return true
		}
		leading = false
	}
	return false
}

func validateVaultPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return cli.MisuseError("a Vault path is required")
	}
	if reservedVaultPath(path) {
		return cli.AuthError("this path is reserved by the platform")
	}
	return nil
}

func validateVaultTarget(args []string, path string) error {
	if len(args) > 1 || len(args) == 1 && path != "" || len(args) == 0 && path == "" {
		return cli.MisuseError("provide either NAME or --path PATH")
	}
	if path != "" {
		return validateVaultPath(path)
	}
	return nil
}

func resolveVaultPath(ctx context.Context, api client.Interface, args []string, path string) (string, error) {
	if len(args) == 1 {
		s, _, err := api.GetSecret(ctx, args[0])
		if err != nil {
			return "", err
		}
		path = s.SecretPath
	}
	return path, validateVaultPath(path)
}
