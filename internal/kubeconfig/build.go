// Package kubeconfig assembles kubectl-compatible kubeconfig files from the
// {endpoint, certificateAuthority} envelope the kupe-api exposes, and merges
// them into the user's existing $KUBECONFIG / ~/.kube/config.
//
// Two auth modes:
//
//  1. Token mode (Build): embed the current API token as a bearer token.
//     Simple; kubeconfig is a secret that expires with the API key.
//  2. Exec mode (BuildExec): emit an exec-plugin user entry that calls back
//     to `kupe auth get-token --context=NAME`. Kubeconfig contains no
//     secret; the CLI resolves the token on each kubectl invocation.
//
// We never hand-roll YAML — every write goes through
// k8s.io/client-go/tools/clientcmd.
package kubeconfig

import (
	"encoding/base64"
	"fmt"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	v1 "k8s.io/client-go/tools/clientcmd/api/v1"
)

// Names bundles the three identity fields kubectl splits across clusters/
// users/contexts. Default: kupe-<tenant>-<cluster> for all three, with
// individual overrides via --cluster-name / --user-name / --context-name.
type Names struct {
	Cluster string
	User    string
	Context string
}

// DefaultNames returns the names used when the caller doesn't override.
func DefaultNames(tenant, cluster string) Names {
	base := fmt.Sprintf("kupe-%s-%s", tenant, cluster)
	return Names{Cluster: base, User: base, Context: base}
}

// BuildTokenConfig returns a fresh kubeconfig embedding the given bearer
// token in the user entry. endpoint and caB64 come from the kupe-api's
// cluster/kubeconfig endpoint — CA is base64-encoded PEM.
func BuildTokenConfig(names Names, endpoint, caB64, token string) (*clientcmdapi.Config, error) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[names.Cluster] = &clientcmdapi.Cluster{
		Server:                   endpoint,
		CertificateAuthorityData: decodeCA(caB64),
	}
	cfg.AuthInfos[names.User] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	cfg.Contexts[names.Context] = &clientcmdapi.Context{
		Cluster:  names.Cluster,
		AuthInfo: names.User,
	}
	cfg.CurrentContext = names.Context
	return cfg, nil
}

// BuildExecConfig returns a kubeconfig whose user entry shells out to
// `kupe auth get-token --context=<contextName>`. The emitted config carries
// no secrets — safe to commit to a shared repo. binary is typically "kupe";
// pass an absolute path in tests or exotic PATH situations.
func BuildExecConfig(names Names, endpoint, caB64, binary, kupeContext string) (*clientcmdapi.Config, error) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[names.Cluster] = &clientcmdapi.Cluster{
		Server:                   endpoint,
		CertificateAuthorityData: decodeCA(caB64),
	}
	cfg.AuthInfos[names.User] = &clientcmdapi.AuthInfo{
		Exec: &clientcmdapi.ExecConfig{
			APIVersion:      "client.authentication.k8s.io/v1",
			Command:         binary,
			Args:            []string{"auth", "get-token", "--context=" + kupeContext},
			InteractiveMode: clientcmdapi.IfAvailableExecInteractiveMode,
		},
	}
	cfg.Contexts[names.Context] = &clientcmdapi.Context{
		Cluster:  names.Cluster,
		AuthInfo: names.User,
	}
	cfg.CurrentContext = names.Context
	return cfg, nil
}

// Marshal converts a kubeconfig to the v1 YAML representation as bytes.
// Uses sigs.k8s.io/yaml (via clientcmd's latest scheme) so output matches
// what `kubectl config view` produces.
func Marshal(cfg *clientcmdapi.Config) ([]byte, error) {
	// Convert internal type → v1 (on-the-wire) type.
	var v1cfg v1.Config
	if err := clientcmdlatest.Scheme.Convert(cfg, &v1cfg, nil); err != nil {
		return nil, fmt.Errorf("converting kubeconfig to v1: %w", err)
	}
	// Ensure the top-level apiVersion/kind survive marshalling.
	v1cfg.APIVersion = "v1"
	v1cfg.Kind = "Config"

	return yamlMarshal(&v1cfg)
}

// decodeCA accepts either a base64-encoded PEM blob (the format kupe-api
// returns) or raw PEM bytes (defensive — support future API versions that
// might stop encoding). Empty input produces nil and kubectl falls back to
// system roots.
func decodeCA(caB64 string) []byte {
	if caB64 == "" {
		return nil
	}
	// Real responses will always be base64; be forgiving if the caller
	// already decoded.
	if b, err := base64.StdEncoding.DecodeString(caB64); err == nil {
		return b
	}
	return []byte(caB64)
}
