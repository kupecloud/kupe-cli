// Package auth owns token storage for the kupe CLI. Tokens never live in the
// main config file — they're stored in the OS keyring (Keychain, Secret
// Service, Credential Manager) or, as a fallback on systems without a
// keyring, in a separate credentials.yaml file with mode 0600.
//
// See docs/auth.md for the full design.
package auth

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Service is the keyring service key used for every context. Account keys
// are the context name. Domain-reversed to keep the namespace collision-safe
// against other tools that might use a generic "kupe" service key — e.g.,
// a hypothetical future `kupe-operator` sharing the host keyring.
const Service = "cloud.kupe.cli"

// ErrKeyringUnavailable is returned by the keyring backend when the OS
// doesn't expose a working secrets API — e.g., a headless Linux box without
// libsecret, or a Secret Service whose D-Bus session bus can't be reached.
// Callers can fall back to plaintext or surface the error depending on
// KUPE_STORAGE policy.
//
// Classification note: zalando/go-keyring only returns its typed
// ErrUnsupportedPlatform from the no-op fallback provider compiled on
// genuinely unsupported OSes. On Linux the Secret Service provider is always
// compiled in and surfaces *raw* D-Bus errors (session-bus dial failure,
// org.freedesktop.secrets not provided) when no secret service is running.
// So realKeyring treats any keyring error that is not ErrNotFound and not
// the size-rejection sentinel as "keyring unavailable", which is what makes
// the documented keyring→plaintext fallback fire on headless Linux/WSL/CI.
var ErrKeyringUnavailable = errors.New("keyring unavailable")

// ErrKeyringTooSmall is returned when the OS keyring rejects the value as
// too large. macOS Keychain caps a single generic-password item at
// ~3000 bytes (service + user + password combined), which OIDC token sets
// can exceed when the JWT carries verbose custom claims. The Manager
// treats this the same as ErrKeyringUnavailable and falls back to
// plaintext, so users on macOS aren't dead-ended on a fresh OIDC login.
var ErrKeyringTooSmall = errors.New("keyring rejected value as too large")

// keyringAPI is the seam the real keyring library plugs into; tests replace
// it with an in-memory implementation.
type keyringAPI interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// Low-level keyring functions, indirected through package vars so tests can
// inject raw backend errors (e.g. a D-Bus connect failure) and assert the
// classification into ErrKeyringUnavailable that drives the plaintext
// fallback. Production points these at zalando/go-keyring.
var (
	keyringGet    = keyring.Get
	keyringSet    = keyring.Set
	keyringDelete = keyring.Delete
)

// realKeyring is the production implementation backed by zalando/go-keyring.
type realKeyring struct{}

func (realKeyring) Get(service, user string) (string, error) {
	v, err := keyringGet(service, user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		// Any other error (ErrUnsupportedPlatform on a fallback build, or a
		// raw D-Bus/Secret-Service error on Linux) means the backend can't
		// serve us — classify as unavailable so the Manager can fall back.
		return "", fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
	}
	return v, nil
}

func (realKeyring) Set(service, user, password string) error {
	if err := keyringSet(service, user, password); err != nil {
		if errors.Is(err, keyring.ErrSetDataTooBig) {
			return ErrKeyringTooSmall
		}
		// Everything else (ErrUnsupportedPlatform, or a raw D-Bus connect /
		// org.freedesktop.secrets name error on headless Linux) is treated as
		// keyring-unavailable so the default policy falls back to plaintext.
		return fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
	}
	return nil
}

func (realKeyring) Delete(service, user string) error {
	if err := keyringDelete(service, user); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // idempotent delete
		}
		return fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
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
