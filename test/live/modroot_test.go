package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/kupecloud/kupe-cli"

// findModuleRoot walks up from start to the first directory whose go.mod
// declares the kupe-cli module. It keys on go.mod, not the directory name,
// so it works from any checkout or git worktree (for example
// .worktrees/<stream>/cli-80). The helper lives in a _test.go file without
// the live tag so the unit-test layer exercises it.
func findModuleRoot(start string) (string, error) {
	dir := start
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil { //#nosec G304 -- path built from the working directory
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && f[0] == "module" && f[1] == modulePath {
					return dir, nil
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate %s module root (go.mod) from %s", modulePath, start)
		}
		dir = parent
	}
}

func TestFindModuleRoot(t *testing.T) {
	write := func(t *testing.T, dir, mod string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("finds the root in a directory not named kupe-cli", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, ".worktrees", "stream", "cli-80")
		write(t, root, "module "+modulePath+"\n\ngo 1.26\n")
		start := filepath.Join(root, "test", "live")
		if err := os.MkdirAll(start, 0o750); err != nil {
			t.Fatal(err)
		}
		got, err := findModuleRoot(start)
		if err != nil {
			t.Fatal(err)
		}
		if got != root {
			t.Fatalf("root = %q, want %q", got, root)
		}
	})

	t.Run("skips a nested go.mod for another module", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "checkout")
		write(t, root, "module "+modulePath+"\n")
		inner := filepath.Join(root, "tools")
		write(t, inner, "module example.com/other\n")
		got, err := findModuleRoot(inner)
		if err != nil {
			t.Fatal(err)
		}
		if got != root {
			t.Fatalf("root = %q, want %q", got, root)
		}
	})

	t.Run("errors when no go.mod declares the module", func(t *testing.T) {
		start := filepath.Join(t.TempDir(), "a", "b")
		write(t, start, "module example.com/other\n")
		if _, err := findModuleRoot(start); err == nil {
			t.Fatal("expected an error")
		}
	})
}
