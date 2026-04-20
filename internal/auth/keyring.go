// Package auth owns token storage for the kupe CLI. Tokens never live in the
// main config file — they're stored in the OS keyring (Keychain, Secret
// Service, Credential Manager) or, as a fallback on systems without a
// keyring, in a separate credentials.yaml file with mode 0600.
//
// See docs/auth.md for the full design.
package auth

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// Service is the keyring service key used for every context. Account keys
// are the context name. Domain-reversed to keep the namespace collision-safe
// against other tools that might use a generic "kupe" service key — e.g.,
// a hypothetical future `kupe-operator` sharing the host keyring.
const Service = "cloud.kupe.cli"

// ErrKeyringUnavailable is returned by the keyring backend when the OS
// doesn't expose a working secrets API — e.g., a headless Linux box without
// libsecret. Callers can fall back to plaintext or surface the error
// depending on KUPE_STORAGE policy.
var ErrKeyringUnavailable = errors.New("keyring unavailable")

// keyringAPI is the seam the real keyring library plugs into; tests replace
// it with an in-memory implementation.
type keyringAPI interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// realKeyring is the production implementation backed by zalando/go-keyring.
type realKeyring struct{}

func (realKeyring) Get(service, user string) (string, error) {
	v, err := keyring.Get(service, user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		if errors.Is(err, keyring.ErrUnsupportedPlatform) {
			return "", ErrKeyringUnavailable
		}
		return "", err
	}
	return v, nil
}

func (realKeyring) Set(service, user, password string) error {
	if err := keyring.Set(service, user, password); err != nil {
		if errors.Is(err, keyring.ErrUnsupportedPlatform) {
			return ErrKeyringUnavailable
		}
		return err
	}
	return nil
}

func (realKeyring) Delete(service, user string) error {
	if err := keyring.Delete(service, user); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // idempotent delete
		}
		if errors.Is(err, keyring.ErrUnsupportedPlatform) {
			return ErrKeyringUnavailable
		}
		return err
	}
	return nil
}

// defaultKeyring is the package-wide keyring; test helpers swap this out.
var defaultKeyring keyringAPI = realKeyring{}

// keyringStorage implements Storage against the OS keyring.
type keyringStorage struct{ k keyringAPI }

// NewKeyringStorage returns a Storage backed by the OS keyring.
func NewKeyringStorage() Storage { return &keyringStorage{k: defaultKeyring} }

func (s *keyringStorage) Get(context string) (string, error) {
	return s.k.Get(Service, context)
}

func (s *keyringStorage) Set(context, token string) error {
	return s.k.Set(Service, context, token)
}

func (s *keyringStorage) Delete(context string) error {
	return s.k.Delete(Service, context)
}

func (s *keyringStorage) Kind() string { return "keyring" }
