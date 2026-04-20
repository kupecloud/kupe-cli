package cluster

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

const testPEM = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"

func testPEMB64() string { return base64.StdEncoding.EncodeToString([]byte(testPEM)) }

// seedForKubeconfig extends the standard factoryWith to also:
//   - seed a ClusterKubeconfig in the fake
//   - ensure f.Token() returns a stable value so token-mode succeeds
func seedForKubeconfig(t *testing.T, fake *clienttest.Fake, name string) *cli.Factory {
	t.Helper()
	if fake.Clusters == nil {
		t.Fatal("fake.Clusters is nil — call clienttest.New")
	}
	fake.Clusters[name] = &client.Cluster{
		Name:   name,
		Type:   "shared",
		Status: &client.ClusterStatus{Phase: client.PhaseRunning},
	}
	fake.Kubeconfigs[name] = &client.ClusterKubeconfig{
		Endpoint:             "https://prod.example:6443",
		CertificateAuthority: testPEMB64(),
	}

	f := factoryWith(t, fake)
	// Override Token so the factory doesn't need to look up keyring.
	f.Token = func() (string, error) { return "kupe_test_token", nil }
	return f
}

func TestKubeconfigStdoutTokenMode(t *testing.T) {
	f := seedForKubeconfig(t, clienttest.New(), "prod")
	cmd := newKubeconfigCmd(f)
	if err := executeCmd(cmd, "prod"); err != nil {
		t.Fatalf("kubeconfig: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, want := range []string{
		"apiVersion: v1",
		"server: https://prod.example:6443",
		"token: kupe_test_token",
		"name: kupe-acme-prod",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestKubeconfigExecModeStripsToken(t *testing.T) {
	f := seedForKubeconfig(t, clienttest.New(), "prod")
	cmd := newKubeconfigCmd(f)
	if err := executeCmd(cmd, "prod", "--exec"); err != nil {
		t.Fatalf("kubeconfig --exec: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	if strings.Contains(out, "kupe_test_token") {
		t.Fatalf("exec kubeconfig leaked token:\n%s", out)
	}
	for _, want := range []string{
		"exec:",
		"apiVersion: client.authentication.k8s.io/v1",
		"- auth",
		"- get-token",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exec output missing %q:\n%s", want, out)
		}
	}
}

func TestKubeconfigMergeIntoEmptyFile(t *testing.T) {
	kcPath := filepath.Join(t.TempDir(), "kc.yaml")
	t.Setenv("KUBECONFIG", kcPath)

	f := seedForKubeconfig(t, clienttest.New(), "prod")
	cmd := newKubeconfigCmd(f)
	if err := executeCmd(cmd, "prod", "--merge"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	data, err := os.ReadFile(kcPath) //nolint:gosec
	if err != nil {
		t.Fatalf("reading merged kubeconfig: %v", err)
	}
	merged := string(data)
	if !strings.Contains(merged, "name: kupe-acme-prod") {
		t.Fatalf("merged file missing context:\n%s", merged)
	}
	if !strings.Contains(merged, "current-context: kupe-acme-prod") {
		t.Fatalf("merged file missing currentContext:\n%s", merged)
	}
}

func TestKubeconfigUnavailableMapsToExit7(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{Name: "prod"}
	fake.GetClusterKubeconfigErr = &client.APIError{StatusCode: 503, Message: "not yet ready"}

	f := factoryWith(t, fake)
	f.Token = func() (string, error) { return "tok", nil }

	cmd := newKubeconfigCmd(f)
	err := executeCmd(cmd, "prod")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitUnavailable {
		t.Fatalf("exit = %d; want %d", code, cli.ExitUnavailable)
	}
}

func TestKubeconfigNotFoundMapsToExit4(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	f.Token = func() (string, error) { return "tok", nil }
	cmd := newKubeconfigCmd(f)
	err := executeCmd(cmd, "ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}

func TestResolveNamesMirrorsContextOntoOthers(t *testing.T) {
	got := resolveNames("acme", "prod", &kubeconfigOpts{contextName: "my-ctx"})
	if got.Context != "my-ctx" || got.User != "my-ctx" || got.Cluster != "my-ctx" {
		t.Fatalf("context override did not mirror: %+v", got)
	}
	// Explicit user-name wins over mirroring.
	got = resolveNames("acme", "prod", &kubeconfigOpts{contextName: "my-ctx", userName: "my-user"})
	if got.User != "my-user" {
		t.Fatalf("--user-name ignored: %+v", got)
	}
}

// compile-time: the cluster_test.go executeCmd helper is reused here.
var _ = func() *cobra.Command { return (&cobra.Command{}) }
var _ = context.Background
