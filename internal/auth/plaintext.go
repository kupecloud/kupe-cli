package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// credentialsFile is the on-disk schema for the plaintext fallback. Kept in a
// separate file from config.yaml so operators can `cat ~/.config/kupe/config.yaml`
// in a support channel without leaking secrets.
type credentialsFile struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Tokens     map[string]string `yaml:"tokens,omitempty"`
}

const (
	credsAPIVersion = "kupe.cloud/v1"
	credsKind       = "Credentials"
)

// plaintextStorage implements Storage against a YAML file at the given path.
// File mode is always 0600; directory mode 0700.
type plaintextStorage struct{ path string }

// NewPlaintextStorage returns a Storage backed by a plaintext credentials
// file. Path defaults to ~/.config/kupe/credentials.yaml when configPath is
// empty (co-located with the main config).
func NewPlaintextStorage(path string) Storage { return &plaintextStorage{path: path} }

// DefaultCredentialsPath returns the credentials-file path that sits next to
// the main config file. Callers derive this from config.DefaultPath.
func DefaultCredentialsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "credentials.yaml")
}

func (s *plaintextStorage) load() (*credentialsFile, error) {
	data, err := os.ReadFile(s.path) //#nosec G304 -- path is derived from user's config dir, by design
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &credentialsFile{APIVersion: credsAPIVersion, Kind: credsKind, Tokens: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}
	f := &credentialsFile{}
	if err := yaml.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.path, err)
	}
	if f.APIVersion != "" && f.APIVersion != credsAPIVersion {
		return nil, fmt.Errorf("%s: unknown apiVersion %q", s.path, f.APIVersion)
	}
	if f.Tokens == nil {
		f.Tokens = map[string]string{}
	}
	return f, nil
}

func (s *plaintextStorage) save(f *credentialsFile) error {
	f.APIVersion = credsAPIVersion
	f.Kind = credsKind
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshalling credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
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
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming %s to %s: %w", tmpName, s.path, err)
	}
	return nil
}

func (s *plaintextStorage) Get(context string) (string, error) {
	f, err := s.load()
	if err != nil {
		return "", err
	}
	tok, ok := f.Tokens[context]
	if !ok {
		return "", ErrNotFound
	}
	return tok, nil
}

func (s *plaintextStorage) Set(context, token string) error {
	f, err := s.load()
	if err != nil {
		return err
	}
	f.Tokens[context] = token
	return s.save(f)
}

func (s *plaintextStorage) Delete(context string) error {
	f, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := f.Tokens[context]; !ok {
		return nil
	}
	delete(f.Tokens, context)
	return s.save(f)
}

func (s *plaintextStorage) Kind() string { return "plaintext" }
