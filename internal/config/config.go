package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// configWarnWriter is where Load emits non-fatal config warnings (unknown
// keys). Defaults to os.Stderr; tests swap it out. Kept as a package var so
// Load doesn't need an IOStreams dependency.
var configWarnWriter io.Writer = os.Stderr

// DefaultPath returns the path the CLI uses for its config file when no
// --config / $KUPE_CONFIG override is set. Respects $XDG_CONFIG_HOME on
// Linux, %AppData% on Windows; falls back to ~/.config/kupe/config.yaml.
func DefaultPath() (string, error) {
	if p := os.Getenv("KUPE_CONFIG"); p != "" {
		return p, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "kupe", "config.yaml"), nil
	}
	if runtime.GOOS == "windows" {
		if app := os.Getenv("AppData"); app != "" {
			return filepath.Join(app, "kupe", "config.yaml"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "kupe", "config.yaml"), nil
}

// Load reads the config file at path. A missing file returns an empty Config
// (APIVersion + Kind populated, no contexts) and no error — this is the
// "first run" case. Any other I/O or parse error is returned.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //#nosec G304 -- path comes from --config/KUPE_CONFIG/platform default, by design
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	cfg := New()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Second, strict pass to catch typo'd keys (e.g. `currentContex`) that the
	// lenient unmarshal above silently ignores. Surfaced as a warning rather
	// than a hard error so a config written by a newer CLI (with fields this
	// version doesn't know) still loads — forward-compatible (KC-16).
	if unknown := unknownConfigKeys(data); len(unknown) > 0 {
		fmt.Fprintf(configWarnWriter, "warning: %s: ignoring unknown config key(s): %s\n", path, strings.Join(unknown, ", "))
	}

	if cfg.APIVersion != "" && cfg.APIVersion != APIVersion {
		return nil, fmt.Errorf("%s: unknown apiVersion %q (expected %q)", path, cfg.APIVersion, APIVersion)
	}
	if cfg.Kind != "" && cfg.Kind != Kind {
		return nil, fmt.Errorf("%s: unknown kind %q (expected %q)", path, cfg.Kind, Kind)
	}

	// Normalise fields that were omitted on disk.
	if cfg.APIVersion == "" {
		cfg.APIVersion = APIVersion
	}
	if cfg.Kind == "" {
		cfg.Kind = Kind
	}

	return cfg, nil
}

// unknownConfigKeys returns the names of top-level (or nested) keys present in
// the YAML that the Config schema does not define. It runs a strict decode
// (KnownFields(true)) and extracts the offending field names from the decoder
// error. Returns nil on a clean decode or if the error can't be parsed.
func unknownConfigKeys(data []byte) []string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var probe Config
	err := dec.Decode(&probe)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	var unknown []string
	for _, line := range strings.Split(err.Error(), "\n") {
		const marker = "not found in type"
		if i := strings.Index(line, "field "); i >= 0 && strings.Contains(line, marker) {
			rest := line[i+len("field "):]
			if j := strings.Index(rest, " "); j > 0 {
				unknown = append(unknown, rest[:j])
			}
		}
	}
	return unknown
}

// Save writes the config atomically to path: marshal → temp file with mode
// 0600 → rename into place. Creates parent directories as mode 0700 if
// missing. A partial write (temp file flushed but rename fails) leaves the
// original file untouched.
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Ensure mode 0600 before writing — on some systems CreateTemp uses 0600
	// already, but we set it explicitly for belt-and-braces.
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}
