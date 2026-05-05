package auth

import (
	"encoding/json"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientauthv1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// newGetTokenCmd is the exec-plugin endpoint called by kubectl when a
// kubeconfig generated via `kupe cluster kubeconfig --exec` is in use.
// It is not intended for direct human use; the help text says as much.
//
// Protocol is the kubectl client-go credential-plugin contract:
// https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins
//
// The command prints a client.authentication.k8s.io/v1 ExecCredential
// object (JSON) to stdout. kubectl consumes it and uses the .status.token
// value as the bearer token.
func newGetTokenCmd(f *cli.Factory) *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:   "get-token",
		Short: "Emit an ExecCredential for kubectl exec-plugin kubeconfigs",
		Long: `Resolve the current Kupe API token and emit it as a
client.authentication.k8s.io/v1 ExecCredential JSON object on stdout.

This command is invoked automatically by kubectl when it authenticates
against a kubeconfig produced by "kupe cluster kubeconfig --exec". Humans
never call it directly.`,
		Hidden: true, // not part of the typical help surface
		RunE: func(_ *cobra.Command, _ []string) error {
			// Ensure --context overrides the factory's resolved context so
			// multi-context setups get the right token.
			if contextName != "" {
				f.Flags.Context = contextName
			}

			tok, expiry, err := f.TokenWithExpiry()
			if err != nil {
				return cli.AuthError("no Kupe credentials available for this context")
			}

			status := &clientauthv1.ExecCredentialStatus{Token: tok}
			// kubectl uses ExpirationTimestamp to cache the credential
			// across requests. Apikey contexts (no expiry) leave it nil
			// so kubectl re-invokes us each time — fine because that
			// path is already a cheap keyring read.
			if !expiry.IsZero() {
				ts := metav1.NewTime(expiry)
				status.ExpirationTimestamp = &ts
			}
			cred := clientauthv1.ExecCredential{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "client.authentication.k8s.io/v1",
					Kind:       "ExecCredential",
				},
				Status: status,
			}
			enc := json.NewEncoder(f.IOStreams.Out)
			enc.SetIndent("", "  ")
			return enc.Encode(cred)
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Kupe context name to resolve the token from (set by the kubeconfig's exec stanza)")
	return cmd
}
