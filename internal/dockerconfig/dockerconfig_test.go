package dockerconfig

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The test binary doubles as a fake credential helper: copied into a
// directory on PATH as docker-credential-<name>, it records each call
// (action and stdin) as a JSON line in $FAKE_HELPER_LOG. With
// FAKE_HELPER_FAIL set it fails, echoing its stdin to stderr (so a secret
// sent to it would appear there); with FAKE_HELPER_EMPTY set, erase answers
// as a helper holding nothing.
func TestMain(m *testing.M) {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "docker-credential-") {
		os.Exit(fakeHelper())
	}
	os.Exit(m.Run())
}

func fakeHelper() int {
	stdin, _ := io.ReadAll(os.Stdin)
	action := ""
	if len(os.Args) > 1 {
		action = os.Args[1]
	}
	if log := os.Getenv("FAKE_HELPER_LOG"); log != "" {
		line, _ := json.Marshal(map[string]string{"helper": filepath.Base(os.Args[0]), "action": action, "stdin": string(stdin)})
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
	if os.Getenv("FAKE_HELPER_FAIL") != "" {
		fmt.Fprintf(os.Stderr, "could not store %s", stdin)
		return 1
	}
	if action == "erase" && os.Getenv("FAKE_HELPER_EMPTY") != "" {
		fmt.Println(notFoundMessage)
		return 1
	}
	return 0
}

type helperCall struct{ Helper, Action, Stdin string }

// installHelper puts the test binary on PATH as docker-credential-<name>
// and returns where its calls are logged.
func installHelper(t *testing.T, name string) (log string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	program := Helper{Name: name}.Program()
	data, err := os.ReadFile(self) // #nosec G304 -- the test binary itself
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, program), data, 0o700); err != nil { // #nosec G306 -- an executable for this test
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	log = filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv("FAKE_HELPER_LOG", log)
	return log
}

func readCalls(t *testing.T, log string) []helperCall {
	t.Helper()
	f, err := os.Open(log) // #nosec G304 -- the test's own log
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var calls []helperCall
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c map[string]string
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, helperCall{c["helper"], c["action"], c["stdin"]})
	}
	return calls
}

// writeConfig writes body as dir/config.json with mode and returns its path.
func writeConfig(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test's own file
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return out
}

var cred = Credential{ServerURL: "registry.kupe.cloud", Username: "kupe+acme+cli-0123456789abcdef", Secret: "kupecr_secretvalue"}

func TestPath_DockerOrder(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "/env/docker")
	if got, _ := Path("/flag/dir"); got != filepath.Join("/flag/dir", FileName) {
		t.Errorf("flag: %s", got)
	}
	if got, _ := Path(""); got != filepath.Join("/env/docker", FileName) {
		t.Errorf("env: %s", got)
	}
	t.Setenv("DOCKER_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got, _ := Path(""); got != filepath.Join(home, ".docker", FileName) {
		t.Errorf("home: %s", got)
	}
}

func TestHelperFor_Order(t *testing.T) {
	tests := []struct {
		name, body, want, key string
	}{
		{"credHelpers for the host wins", `{"credsStore":"desktop","credHelpers":{"registry.kupe.cloud":"pass","ghcr.io":"gh"}}`, "pass", `credHelpers["registry.kupe.cloud"]`},
		{"credsStore when the host has no credHelper", `{"credsStore":"desktop","credHelpers":{"ghcr.io":"gh"}}`, "desktop", "credsStore"},
		{"none", `{"auths":{}}`, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Load(writeConfig(t, tt.body, 0o600))
			if err != nil {
				t.Fatal(err)
			}
			h, err := f.HelperFor("registry.kupe.cloud")
			if err != nil || h.Name != tt.want || h.Key != tt.key {
				t.Fatalf("helper = %+v, %v; want %q from %q", h, err, tt.want, tt.key)
			}
		})
	}
}

func TestStore_HelperGetsTheExactJSONOnStdin(t *testing.T) {
	log := installHelper(t, "kupetest")
	// An older plain login for the host, built here so no encoded value
	// sits in the source.
	oldAuth := base64.StdEncoding.EncodeToString([]byte("old:old"))
	path := writeConfig(t, fmt.Sprintf(`{"credsStore":"kupetest","auths":{"registry.kupe.cloud":{"auth":%q},"ghcr.io":{"auth":"Z2g="}}}`, oldAuth), 0o600)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.Store(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Helper.Name != "kupetest" {
		t.Fatalf("stored = %+v", stored)
	}
	calls := readCalls(t, log)
	want := `{"ServerURL":"registry.kupe.cloud","Username":"kupe+acme+cli-0123456789abcdef","Secret":"kupecr_secretvalue"}`
	if len(calls) != 1 || calls[0].Action != "store" || calls[0].Stdin != want {
		t.Fatalf("calls = %+v", calls)
	}
	// The old plain entry for the host is emptied, as docker login does;
	// another registry's entry is untouched.
	auths := readJSON(t, path)["auths"].(map[string]any)
	if entry := auths["registry.kupe.cloud"].(map[string]any); len(entry) != 0 {
		t.Fatalf("host entry = %v; want {}", entry)
	}
	if auths["ghcr.io"].(map[string]any)["auth"] != "Z2g=" {
		t.Fatalf("other entry changed: %v", auths["ghcr.io"])
	}
	if raw, _ := os.ReadFile(path); strings.Contains(string(raw), cred.Secret) { // #nosec G304 -- the test's own file
		t.Fatal("secret written to the config despite a helper")
	}
}

func TestStore_HelperFailureScrubsTheSecret(t *testing.T) {
	installHelper(t, "kupetest")
	t.Setenv("FAKE_HELPER_FAIL", "1")
	f, err := Load(writeConfig(t, `{"credsStore":"kupetest"}`, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Store(context.Background(), cred)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), cred.Secret) || !strings.Contains(err.Error(), "***") || !strings.Contains(err.Error(), "docker-credential-kupetest store failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestStore_InvalidHelperName(t *testing.T) {
	for _, name := range []string{"../evil", "-x", ".hidden", "a b", "a;b", "a/b"} {
		f, err := Load(writeConfig(t, fmt.Sprintf(`{"credsStore":%q}`, name), 0o600))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Store(context.Background(), cred); !errors.Is(err, ErrInvalidHelper) {
			t.Errorf("%q: err = %v; want ErrInvalidHelper", name, err)
		}
	}
}

func TestStore_MissingHelperNamesIt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	f, err := Load(writeConfig(t, `{"credHelpers":{"registry.kupe.cloud":"nothere"}}`, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Store(context.Background(), cred)
	if !errors.Is(err, ErrHelperNotFound) || !strings.Contains(err.Error(), "docker-credential-nothere") ||
		!strings.Contains(err.Error(), `credHelpers["registry.kupe.cloud"]`) {
		t.Fatalf("err = %v", err)
	}
}

func TestStore_PlainKeepsEverythingElse(t *testing.T) {
	body := `{"auths":{"ghcr.io":{"auth":"Z2g=","email":"x@y"}},"proxies":{"default":{"httpProxy":"http://p:3128"}},"currentContext":"colima","psFormat":"table {{.ID}}"}`
	path := writeConfig(t, body, 0o600)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.Store(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Helper.Name != "" || stored.Path != path {
		t.Fatalf("stored = %+v", stored)
	}
	got := readJSON(t, path)
	var before map[string]any
	_ = json.Unmarshal([]byte(body), &before)
	for _, key := range []string{"proxies", "currentContext", "psFormat"} {
		if fmt.Sprint(got[key]) != fmt.Sprint(before[key]) {
			t.Errorf("%s = %v; want %v", key, got[key], before[key])
		}
	}
	auths := got["auths"].(map[string]any)
	if fmt.Sprint(auths["ghcr.io"]) != fmt.Sprint(before["auths"].(map[string]any)["ghcr.io"]) {
		t.Errorf("ghcr.io entry changed: %v", auths["ghcr.io"])
	}
	want := base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Secret))
	if auths["registry.kupe.cloud"].(map[string]any)["auth"] != want {
		t.Fatalf("host entry = %v", auths["registry.kupe.cloud"])
	}
}

func TestStore_PlainFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	t.Run("a new file is 0600 in a new 0700 directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "new", FileName)
		f, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Store(context.Background(), cred); err != nil {
			t.Fatal(err)
		}
		assertMode(t, path, 0o600)
		assertMode(t, filepath.Dir(path), 0o700)
	})
	t.Run("an existing 0600 file stays 0600", func(t *testing.T) {
		path := writeConfig(t, `{}`, 0o600)
		storeAt(t, path)
		assertMode(t, path, 0o600)
	})
	t.Run("a wider file is narrowed to 0600", func(t *testing.T) {
		path := writeConfig(t, `{}`, 0o644)
		storeAt(t, path)
		assertMode(t, path, 0o600)
	})
	t.Run("a narrower file is never widened", func(t *testing.T) {
		path := writeConfig(t, `{}`, 0o400)
		storeAt(t, path)
		assertMode(t, path, 0o400)
	})
	t.Run("no temporary file is left behind", func(t *testing.T) {
		path := writeConfig(t, `{}`, 0o600)
		storeAt(t, path)
		entries, _ := os.ReadDir(filepath.Dir(path))
		if len(entries) != 1 {
			t.Fatalf("dir holds %d entries; want only config.json", len(entries))
		}
	})
}

func TestStore_SymlinkedConfigWritesItsTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	target := writeConfig(t, `{"psFormat":"x"}`, 0o600)
	link := filepath.Join(t.TempDir(), FileName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	storeAt(t, link)
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", info, err)
	}
	if readJSON(t, target)["auths"] == nil {
		t.Fatal("target not written")
	}
}

func TestLoad_RefusesACorruptFile(t *testing.T) {
	path := writeConfig(t, `{"auths":`, 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
}

func TestErase(t *testing.T) {
	t.Run("plain removes the entry only", func(t *testing.T) {
		path := writeConfig(t, `{"auths":{"registry.kupe.cloud":{"auth":"eA=="},"ghcr.io":{"auth":"Z2g="}}}`, 0o600)
		f, _ := Load(path)
		erased, err := f.Erase(context.Background(), "registry.kupe.cloud")
		if err != nil || !erased.Found {
			t.Fatalf("erased = %+v, %v", erased, err)
		}
		auths := readJSON(t, path)["auths"].(map[string]any)
		if _, ok := auths["registry.kupe.cloud"]; ok || auths["ghcr.io"] == nil {
			t.Fatalf("auths = %v", auths)
		}
	})
	t.Run("nothing stored is not an error", func(t *testing.T) {
		f, _ := Load(writeConfig(t, `{}`, 0o600))
		erased, err := f.Erase(context.Background(), "registry.kupe.cloud")
		if err != nil || erased.Found {
			t.Fatalf("erased = %+v, %v", erased, err)
		}
	})
	t.Run("helper gets the server URL on stdin", func(t *testing.T) {
		log := installHelper(t, "kupetest")
		path := writeConfig(t, `{"credsStore":"kupetest","auths":{"registry.kupe.cloud":{}}}`, 0o600)
		f, _ := Load(path)
		erased, err := f.Erase(context.Background(), "registry.kupe.cloud")
		if err != nil || !erased.Found || erased.Helper.Name != "kupetest" {
			t.Fatalf("erased = %+v, %v", erased, err)
		}
		calls := readCalls(t, log)
		if len(calls) != 1 || calls[0].Action != "erase" || calls[0].Stdin != "registry.kupe.cloud" {
			t.Fatalf("calls = %+v", calls)
		}
		if _, ok := readJSON(t, path)["auths"].(map[string]any)["registry.kupe.cloud"]; ok {
			t.Fatal("auths entry kept")
		}
	})
	t.Run("a helper holding nothing is not an error", func(t *testing.T) {
		installHelper(t, "kupetest")
		t.Setenv("FAKE_HELPER_EMPTY", "1")
		f, _ := Load(writeConfig(t, `{"credsStore":"kupetest"}`, 0o600))
		erased, err := f.Erase(context.Background(), "registry.kupe.cloud")
		if err != nil || erased.Found {
			t.Fatalf("erased = %+v, %v", erased, err)
		}
	})
}

func storeAt(t *testing.T, path string) {
	t.Helper()
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Store(context.Background(), cred); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o; want %o", path, got, want)
	}
}
