package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWarnsOnUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	// "currentContex" is a typo for "currentContext".
	data := []byte("apiVersion: kupe.cloud/v1\nkind: Config\ncurrentContex: prod\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	orig := configWarnWriter
	configWarnWriter = &buf
	t.Cleanup(func() { configWarnWriter = orig })

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load should not hard-fail on unknown key (forward-compat): %v", err)
	}
	if cfg.CurrentContext != "" {
		t.Fatalf("typo'd key should not apply; CurrentContext = %q", cfg.CurrentContext)
	}
	if !strings.Contains(buf.String(), "currentContex") {
		t.Fatalf("expected unknown-key warning mentioning currentContex; got %q", buf.String())
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing) returned error: %v", err)
	}
	if cfg.APIVersion != APIVersion || cfg.Kind != Kind {
		t.Fatalf("want canonical header, got apiVersion=%q kind=%q", cfg.APIVersion, cfg.Kind)
	}
	if len(cfg.Contexts) != 0 {
		t.Fatalf("want empty contexts, got %d", len(cfg.Contexts))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kupe", "config.yaml")

	waitTrue := true
	in := &Config{
		APIVersion:     APIVersion,
		Kind:           Kind,
		CurrentContext: "prod",
		Contexts: []Context{
			{Name: "prod", APIURL: "https://api.kupe.cloud", Tenant: "acme", TokenRef: TokenRefKeyring, User: "billy@acme.com"},
		},
		Preferences: Preferences{Output: "table", Wait: &waitTrue, WaitTimeout: "30m"},
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File mode should be 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o; want 0600", mode)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.CurrentContext != "prod" || len(out.Contexts) != 1 || out.Contexts[0].Tenant != "acme" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Preferences.Wait == nil || *out.Preferences.Wait != true {
		t.Fatalf("Preferences.Wait round-trip failed: %+v", out.Preferences)
	}
}

func TestLoadRejectsUnknownAPIVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("apiVersion: kupe.cloud/v9\nkind: Config\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown apiVersion") {
		t.Fatalf("want unknown apiVersion error, got %v", err)
	}
}

func TestSaveTokensNeverWrittenToConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{
		APIVersion: APIVersion,
		Kind:       Kind,
		Contexts: []Context{
			{Name: "prod", Tenant: "acme", TokenRef: TokenRefKeyring},
		},
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	// The config schema has no "token" field — ensure the YAML never grows one.
	if strings.Contains(string(data), "token:") && !strings.Contains(string(data), "tokenRef:") {
		t.Fatalf("config file contains a token: field, redaction broken:\n%s", data)
	}
}
