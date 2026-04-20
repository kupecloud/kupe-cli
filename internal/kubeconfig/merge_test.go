package kubeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestTargetPathPrefersExplicit(t *testing.T) {
	got, err := TargetPath("/tmp/explicit-kc.yaml")
	if err != nil || got != "/tmp/explicit-kc.yaml" {
		t.Fatalf("TargetPath(explicit) = %q, %v", got, err)
	}
}

func TestTargetPathReadsKubeconfigEnv(t *testing.T) {
	t.Setenv("KUBECONFIG", "/a/b.yaml:/c/d.yaml")
	got, err := TargetPath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/a/b.yaml" {
		t.Fatalf("TargetPath picked %q; want first entry", got)
	}
}

func TestMergeIntoFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kc.yaml")
	incoming, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod:6443", fakeCAB64(), "kupe_a")

	if err := Merge(path, incoming, MergeOptions{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	loaded, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if loaded.Contexts["kupe-acme-prod"] == nil {
		t.Fatalf("context missing after merge; got %+v", loaded)
	}
	if loaded.CurrentContext != "kupe-acme-prod" {
		t.Errorf("currentContext = %q", loaded.CurrentContext)
	}
}

func TestMergePreservesOtherContexts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kc.yaml")

	// Seed with a foreign context we must not clobber.
	seed := clientcmdapi.NewConfig()
	seed.Clusters["other"] = &clientcmdapi.Cluster{Server: "https://other:6443"}
	seed.AuthInfos["other"] = &clientcmdapi.AuthInfo{Token: "other-tok"}
	seed.Contexts["other"] = &clientcmdapi.Context{Cluster: "other", AuthInfo: "other"}
	seed.CurrentContext = "other"
	if err := clientcmd.WriteToFile(*seed, path); err != nil {
		t.Fatal(err)
	}

	incoming, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod:6443", fakeCAB64(), "kupe_a")
	if err := Merge(path, incoming, MergeOptions{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	loaded, _ := clientcmd.LoadFromFile(path)
	if loaded.Contexts["other"] == nil {
		t.Fatal("merge clobbered existing 'other' context")
	}
	if loaded.Contexts["kupe-acme-prod"] == nil {
		t.Fatal("merge did not add new context")
	}
}

func TestMergeIsIdempotentForIdenticalInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kc.yaml")
	incoming, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod:6443", fakeCAB64(), "kupe_a")

	if err := Merge(path, incoming, MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := Merge(path, incoming, MergeOptions{}); err != nil {
		t.Fatalf("second merge (identical input) should succeed, got %v", err)
	}
}

func TestMergeDetectsClusterCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kc.yaml")
	a, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod-a:6443", fakeCAB64(), "tok-a")
	b, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod-b:6443", fakeCAB64(), "tok-b") // different server

	if err := Merge(path, a, MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	err := Merge(path, b, MergeOptions{})
	if err == nil {
		t.Fatal("want collision error")
	}
	if !errors.Is(err, ErrCollision) {
		t.Fatalf("want ErrCollision, got %v", err)
	}

	// With force, the collision is silenced and server is replaced.
	if err := Merge(path, b, MergeOptions{Force: true}); err != nil {
		t.Fatalf("force merge: %v", err)
	}
	loaded, _ := clientcmd.LoadFromFile(path)
	if got := loaded.Clusters["kupe-acme-prod"].Server; got != "https://prod-b:6443" {
		t.Fatalf("server after --force = %s", got)
	}
}

// Fix 2 regression: Merge must refuse to overwrite a corrupt kubeconfig
// by default. Silently starting from an empty config would wipe every
// other context the user has accumulated (a data-loss bug).
func TestMergeRefusesCorruptFileByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kc.yaml")
	// Seed with garbage — not valid YAML.
	if err := os.WriteFile(path, []byte("this is not :valid: yaml at all:::"), 0o600); err != nil {
		t.Fatal(err)
	}

	incoming, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod:6443", fakeCAB64(), "kupe_a")
	err := Merge(path, incoming, MergeOptions{})
	if err == nil {
		t.Fatal("expected ErrCorrupt, got nil — would have overwritten corrupt file")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}

	// The file on disk must be UNCHANGED — no silent clobber.
	after, _ := os.ReadFile(path) //nolint:gosec // test fixture read
	if !strings.Contains(string(after), "not :valid:") {
		t.Fatalf("file was modified despite ErrCorrupt; content: %s", after)
	}
}

// Fix 2: --force-overwrite (MergeOptions.ForceOverwrite) explicitly
// authorises discarding a corrupt file. Test verifies the opt-in path.
func TestMergeOverwritesCorruptFileWithForceOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(path, []byte("garbage:::"), 0o600); err != nil {
		t.Fatal(err)
	}
	incoming, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod:6443", fakeCAB64(), "kupe_a")

	if err := Merge(path, incoming, MergeOptions{ForceOverwrite: true}); err != nil {
		t.Fatalf("force-overwrite should succeed on corrupt file: %v", err)
	}

	loaded, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load after force-overwrite: %v", err)
	}
	if loaded.Contexts["kupe-acme-prod"] == nil {
		t.Fatal("new context missing after force-overwrite")
	}
}

func TestMergeAtomicFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "dir", "kc.yaml")
	incoming, _ := BuildTokenConfig(DefaultNames("acme", "prod"), "https://prod:6443", fakeCAB64(), "kupe_a")

	if err := Merge(path, incoming, MergeOptions{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("kubeconfig mode = %o; want 0600", mode)
	}
}
