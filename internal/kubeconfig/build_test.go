package kubeconfig

import (
	"encoding/base64"
	"strings"
	"testing"
)

const fakePEM = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"

func fakeCAB64() string { return base64.StdEncoding.EncodeToString([]byte(fakePEM)) }

func TestDefaultNamesPattern(t *testing.T) {
	n := DefaultNames("acme", "prod")
	for _, got := range []string{n.Cluster, n.User, n.Context} {
		if got != "kupe-acme-prod" {
			t.Fatalf("DefaultNames mismatch: %+v", n)
		}
	}
}

func TestBuildTokenConfig(t *testing.T) {
	cfg, err := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod.example:6443", fakeCAB64(), "kupe_tok")
	if err != nil {
		t.Fatalf("BuildTokenConfig: %v", err)
	}
	cluster := cfg.Clusters["kupe-acme-prod"]
	if cluster == nil || cluster.Server != "https://prod.example:6443" {
		t.Fatalf("cluster entry wrong: %+v", cluster)
	}
	if string(cluster.CertificateAuthorityData) != fakePEM {
		t.Fatalf("CA not base64-decoded: got %q", cluster.CertificateAuthorityData)
	}
	authInfo := cfg.AuthInfos["kupe-acme-prod"]
	if authInfo == nil || authInfo.Token != "kupe_tok" || authInfo.Exec != nil {
		t.Fatalf("authinfo wrong: %+v", authInfo)
	}
	if cfg.CurrentContext != "kupe-acme-prod" {
		t.Fatalf("currentContext = %q", cfg.CurrentContext)
	}
}

func TestBuildExecConfig(t *testing.T) {
	cfg, err := BuildExecConfig(DefaultNames("acme", "prod"), "https://prod.example:6443", fakeCAB64(), "/usr/local/bin/kupe", "prod")
	if err != nil {
		t.Fatalf("BuildExecConfig: %v", err)
	}
	authInfo := cfg.AuthInfos["kupe-acme-prod"]
	if authInfo == nil || authInfo.Exec == nil {
		t.Fatalf("expected exec stanza, got %+v", authInfo)
	}
	if authInfo.Token != "" {
		t.Fatalf("token leaked into exec-mode kubeconfig: %q", authInfo.Token)
	}
	if authInfo.Exec.Command != "/usr/local/bin/kupe" {
		t.Errorf("command = %q", authInfo.Exec.Command)
	}
	wantArgs := []string{"auth", "get-token", "--context=prod"}
	if len(authInfo.Exec.Args) != 3 {
		t.Fatalf("args = %v; want %v", authInfo.Exec.Args, wantArgs)
	}
	for i, a := range wantArgs {
		if authInfo.Exec.Args[i] != a {
			t.Errorf("args[%d] = %q; want %q", i, authInfo.Exec.Args[i], a)
		}
	}
}

func TestMarshalProducesV1Kubeconfig(t *testing.T) {
	cfg, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod.example:6443", fakeCAB64(), "kupe_x")
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	yaml := string(data)
	for _, want := range []string{
		"apiVersion: v1",
		"kind: Config",
		"name: kupe-acme-prod",
		"server: https://prod.example:6443",
		"token: kupe_x",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("rendered kubeconfig missing %q:\n%s", want, yaml)
		}
	}
}

func TestDecodeCAEmptyAndPlain(t *testing.T) {
	if b := decodeCA(""); b != nil {
		t.Fatalf("empty CA: %v", b)
	}
	// Non-base64 input falls through to raw bytes.
	raw := decodeCA("not-valid-base64-!!")
	if string(raw) != "not-valid-base64-!!" {
		t.Fatalf("raw CA round-trip mismatch: %q", raw)
	}
}
