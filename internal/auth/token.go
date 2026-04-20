package auth

import (
	"errors"
	"os"
)

// ErrNotFound is returned by Storage.Get when no token is stored for the
// given context. Distinct from ErrKeyringUnavailable which signals the whole
// backend is missing.
var ErrNotFound = errors.New("token not found")

// Storage is the abstraction over token-store backends (keyring, plaintext).
// Tests satisfy it with an in-memory map; production wires in the keyring
// and/or plaintext implementations.
type Storage interface {
	Get(context string) (string, error)
	Set(context, token string) error
	Delete(context string) error
	// Kind returns a short identifier ("keyring" / "plaintext") used as the
	// TokenRef written into the main config file.
	Kind() string
}

// Manager picks the right Storage for each operation based on (a) the
// context's existing TokenRef for reads/deletes and (b) the KUPE_STORAGE
// policy for writes.
//
// Policy:
//
//	KUPE_STORAGE=""          → prefer keyring, fall back to plaintext on
//	                            ErrKeyringUnavailable.
//	KUPE_STORAGE=keyring     → keyring only; fail hard if unavailable.
//	KUPE_STORAGE=plaintext   → always plaintext.
type Manager struct {
	keyring   Storage
	plaintext Storage
	policy    string
}

// NewManager wires a Manager with the two backends. plaintextPath is the
// destination for the plaintext fallback (typically from
// DefaultCredentialsPath).
func NewManager(plaintextPath string) *Manager {
	return &Manager{
		keyring:   NewKeyringStorage(),
		plaintext: NewPlaintextStorage(plaintextPath),
		policy:    os.Getenv("KUPE_STORAGE"),
	}
}

// newManagerWith is an internal test helper that lets tests inject fake
// Storage implementations without touching the real OS keyring or filesystem.
func newManagerWith(keyring, plaintext Storage, policy string) *Manager {
	return &Manager{keyring: keyring, plaintext: plaintext, policy: policy}
}

// GetByRef looks up the token for context using the given TokenRef, which
// must match what Set previously wrote into the config's context.
func (m *Manager) GetByRef(context, ref string) (string, error) {
	switch ref {
	case "keyring":
		return m.keyring.Get(context)
	case "plaintext":
		return m.plaintext.Get(context)
	case "":
		return "", ErrNotFound
	default:
		return "", errors.New("unknown tokenRef: " + ref)
	}
}

// Set stores the token for context, respecting the KUPE_STORAGE policy.
// Returns the TokenRef that should be written into the context's config
// entry.
func (m *Manager) Set(context, token string) (ref string, err error) {
	switch m.policy {
	case "plaintext":
		return m.plaintext.Kind(), m.plaintext.Set(context, token)
	case "keyring":
		return m.keyring.Kind(), m.keyring.Set(context, token)
	default:
		// Prefer keyring; fall back to plaintext on ErrKeyringUnavailable.
		if err := m.keyring.Set(context, token); err != nil {
			if errors.Is(err, ErrKeyringUnavailable) {
				return m.plaintext.Kind(), m.plaintext.Set(context, token)
			}
			return "", err
		}
		return m.keyring.Kind(), nil
	}
}

// DeleteByRef removes the token for context using the given TokenRef.
// Idempotent: missing tokens return nil.
func (m *Manager) DeleteByRef(context, ref string) error {
	switch ref {
	case "keyring":
		return m.keyring.Delete(context)
	case "plaintext":
		return m.plaintext.Delete(context)
	case "":
		return nil
	default:
		return errors.New("unknown tokenRef: " + ref)
	}
}
