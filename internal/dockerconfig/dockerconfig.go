// Package dockerconfig stores and erases registry credentials the way
// "docker login" and "docker logout" do: through the credential helper the
// Docker config names for the registry (credHelpers, then credsStore), or,
// with none, base64 in the config file's auths. It reads and writes only
// the keys it needs and keeps every other key of config.json as it was. It
// does not use the Docker CLI's packages: the credential-helper protocol is
// a JSON document on a helper's stdin.
package dockerconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName is the Docker config file's name in its directory.
const FileName = "config.json"

// Credential is one registry login.
type Credential struct {
	// ServerURL is the registry host ("registry.kupe.cloud").
	ServerURL string
	Username  string
	Secret    string
}

// Path is the Docker config file in Docker's own order: dir/config.json when
// dir is set (docker --config DIR), else $DOCKER_CONFIG/config.json, else
// config.json in .docker under the home directory (%USERPROFILE% on
// Windows).
func Path(dir string) (string, error) {
	if dir != "" {
		return filepath.Join(dir, FileName), nil
	}
	if env := os.Getenv("DOCKER_CONFIG"); env != "" {
		return filepath.Join(env, FileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory for the Docker config: %w", err)
	}
	return filepath.Join(home, ".docker", FileName), nil
}

// File is a Docker config file as read. Keys this package does not use are
// kept as raw JSON, so writing it back changes nothing but the entry for the
// registry.
type File struct {
	// Path is the file the config was read from and is written to.
	Path string
	raw  map[string]json.RawMessage
	// mode is the file's permission bits as read; zero if it did not exist.
	mode fs.FileMode
}

// Load reads the Docker config at path. A missing file is an empty config;
// a file that is not a JSON object is an error, so it is never overwritten.
func Load(path string) (*File, error) {
	f := &File{Path: path, raw: map[string]json.RawMessage{}}
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own Docker config, at the path Docker reads
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return f, nil
	case err != nil:
		return nil, fmt.Errorf("reading Docker config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &f.raw); err != nil {
			return nil, fmt.Errorf("parsing Docker config %s: %w; fix or move the file and try again", path, err)
		}
		if f.raw == nil {
			f.raw = map[string]json.RawMessage{}
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading Docker config %s: %w", path, err)
	}
	f.mode = info.Mode().Perm()
	return f, nil
}

// Helper names the credential helper Docker uses for host: Name is "" when
// there is none, and Key is the config key that chose it, for messages.
type Helper struct {
	Name string
	Key  string
}

// HelperFor picks the credential helper for host as Docker does:
// credHelpers[host] if set, else credsStore if set, else none.
func (f *File) HelperFor(host string) (Helper, error) {
	var perHost map[string]string
	if err := f.decode("credHelpers", &perHost); err != nil {
		return Helper{}, err
	}
	if name := perHost[host]; name != "" {
		return Helper{Name: name, Key: fmt.Sprintf("credHelpers[%q]", host)}, nil
	}
	var store string
	if err := f.decode("credsStore", &store); err != nil {
		return Helper{}, err
	}
	if store != "" {
		return Helper{Name: store, Key: "credsStore"}, nil
	}
	return Helper{}, nil
}

// decode reads key into v; an absent key leaves v as it is.
func (f *File) decode(key string, v any) error {
	raw, ok := f.raw[key]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("parsing %q in Docker config %s: %w", key, f.Path, err)
	}
	return nil
}

// auths is the config's auths object, each entry kept as raw JSON.
func (f *File) auths() (map[string]json.RawMessage, error) {
	auths := map[string]json.RawMessage{}
	if err := f.decode("auths", &auths); err != nil {
		return nil, err
	}
	if auths == nil {
		auths = map[string]json.RawMessage{}
	}
	return auths, nil
}

// setAuths writes auths back into the config.
func (f *File) setAuths(auths map[string]json.RawMessage) error {
	raw, err := json.Marshal(auths)
	if err != nil {
		return fmt.Errorf("encoding auths: %w", err)
	}
	f.raw["auths"] = raw
	return nil
}

// Stored says where Store put a credential.
type Stored struct {
	// Helper is the credential helper that holds it; "" when it is in the
	// config file itself (base64, not encrypted).
	Helper Helper
	// Path is the config file written.
	Path string
}

// Store saves cred as docker login does. With a credential helper, the
// helper gets it on stdin and the config's auths entry for the host is
// emptied, so no older plain copy of a secret stays in the file; without
// one, the entry holds base64(username:secret).
func (f *File) Store(ctx context.Context, cred Credential) (Stored, error) {
	helper, err := f.HelperFor(cred.ServerURL)
	if err != nil {
		return Stored{}, err
	}
	entry := json.RawMessage(`{}`)
	if helper.Name != "" {
		// The helper protocol's store document; it goes to the helper's
		// stdin only.
		payload, err := json.Marshal(struct { // #nosec G117 -- the credential-helper protocol carries the secret on stdin by design
			ServerURL string `json:"ServerURL"`
			Username  string `json:"Username"`
			Secret    string `json:"Secret"`
		}{cred.ServerURL, cred.Username, cred.Secret})
		if err != nil {
			return Stored{}, fmt.Errorf("encoding the credential: %w", err)
		}
		if _, err := runHelper(ctx, helper, "store", payload, cred.Secret); err != nil {
			return Stored{}, err
		}
	} else {
		auth := base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Secret))
		if entry, err = json.Marshal(struct {
			Auth string `json:"auth"`
		}{auth}); err != nil {
			return Stored{}, fmt.Errorf("encoding the credential: %w", err)
		}
	}
	auths, err := f.auths()
	if err != nil {
		return Stored{}, err
	}
	auths[cred.ServerURL] = entry
	if err := f.setAuths(auths); err != nil {
		return Stored{}, err
	}
	if err := f.write(); err != nil {
		return Stored{}, err
	}
	return Stored{Helper: helper, Path: f.Path}, nil
}

// Erased says what Erase removed.
type Erased struct {
	// Helper is the credential helper asked to erase; "" when none.
	Helper Helper
	// Found is false when nothing was stored for the host.
	Found bool
}

// Erase removes the credential for host as docker logout does: from the
// credential helper (one that has nothing stored is not an error) and from
// the config's auths.
func (f *File) Erase(ctx context.Context, host string) (Erased, error) {
	helper, err := f.HelperFor(host)
	if err != nil {
		return Erased{}, err
	}
	out := Erased{Helper: helper}
	if helper.Name != "" {
		found, err := eraseFromHelper(ctx, helper, host)
		if err != nil {
			return out, err
		}
		out.Found = found
	}
	auths, err := f.auths()
	if err != nil {
		return out, err
	}
	if _, ok := auths[host]; !ok {
		return out, nil
	}
	delete(auths, host)
	if helper.Name == "" {
		out.Found = true
	}
	if err := f.setAuths(auths); err != nil {
		return out, err
	}
	return out, f.write()
}

// Has reports whether the config's auths has an entry for host.
func (f *File) Has(host string) bool {
	auths, err := f.auths()
	if err != nil {
		return false
	}
	_, ok := auths[host]
	return ok
}

// write saves the config atomically: a temporary file in the same
// directory, then a rename, so a crash never leaves the file truncated. The
// mode is 0600, or narrower when the file already was, never wider. A
// config that is a symbolic link is written where it points, as Docker
// does.
func (f *File) write() error {
	data, err := json.MarshalIndent(f.raw, "", "\t")
	if err != nil {
		return fmt.Errorf("encoding Docker config: %w", err)
	}
	target := f.Path
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	perm := fs.FileMode(0o600)
	if f.mode != 0 {
		perm &= f.mode
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return fmt.Errorf("renaming %s to %s: %w", tmpName, target, err)
	}
	f.mode = perm
	return nil
}
