package auth

import (
	"context"
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
	// credsPath is the plaintext credentials path; the cross-process refresh
	// lock files are co-located with it. Empty in tests that don't exercise
	// RefreshLocked (those use newManagerWith, which leaves locking disabled).
	credsPath string
}

// NewManager wires a Manager with the two backends. plaintextPath is the
// destination for the plaintext fallback (typically from
// DefaultCredentialsPath).
func NewManager(plaintextPath string) *Manager {
	return &Manager{
		keyring:   NewKeyringStorage(),
		plaintext: NewPlaintextStorage(plaintextPath),
		policy:    os.Getenv("KUPE_STORAGE"),
		credsPath: plaintextPath,
	}
}

// newManagerWith is an internal test helper that lets tests inject fake
// Storage implementations without touching the real OS keyring or filesystem.
func newManagerWith(keyring, plaintext Storage, policy string) *Manager {
	return &Manager{keyring: keyring, plaintext: plaintext, policy: policy}
}

// GetByRef looks up the token for ctxName using the given TokenRef, which
// must match what Set previously wrote into the config's context.
func (m *Manager) GetByRef(ctxName, ref string) (string, error) {
	switch ref {
	case "keyring":
		return m.keyring.Get(ctxName)
	case "plaintext":
		return m.plaintext.Get(ctxName)
	case "":
		return "", ErrNotFound
	default:
		return "", errors.New("unknown tokenRef: " + ref)
	}
}

// Set stores the token for context, respecting the KUPE_STORAGE policy.
// Returns the TokenRef that should be written into the context's config
// entry.
//
// In the default policy, both an unavailable keyring (no secret service
// at all) and a keyring that rejects the value as too large fall back
// to the plaintext file. The size-rejection path matters in practice on
// macOS where Keychain caps a single item at ~3KB and OIDC token sets
// with verbose custom claims can exceed that.
func (m *Manager) Set(ctxName, token string) (ref string, err error) {
	switch m.policy {
	case "plaintext":
		return m.plaintext.Kind(), m.plaintext.Set(ctxName, token)
	case "keyring":
		return m.keyring.Kind(), m.keyring.Set(ctxName, token)
	default:
		if err := m.keyring.Set(ctxName, token); err != nil {
			if errors.Is(err, ErrKeyringUnavailable) || errors.Is(err, ErrKeyringTooSmall) {
				return m.plaintext.Kind(), m.plaintext.Set(ctxName, token)
			}
			return "", err
		}
		return m.keyring.Kind(), nil
	}
}

// DeleteByRef removes the token for ctxName using the given TokenRef.
// Idempotent: missing tokens return nil.
func (m *Manager) DeleteByRef(ctxName, ref string) error {
	switch ref {
	case "keyring":
		return m.keyring.Delete(ctxName)
	case "plaintext":
		return m.plaintext.Delete(ctxName)
	case "":
		return nil
	default:
		return errors.New("unknown tokenRef: " + ref)
	}
}

// RefreshLocked performs a cross-process-safe OIDC refresh for a context.
//
// It serialises refreshes for the context behind an advisory file lock so two
// concurrent kupe invocations (routine under the kubectl exec-plugin model)
// can't race the refresh-token rotation. The sequence is:
//
//  1. Acquire the per-context refresh lock.
//  2. Re-read the stored token set. If another process already refreshed while
//     we waited for the lock, return that fresh set without refreshing — this
//     is what stops the loser of a race from spending an already-rotated
//     refresh token and getting invalid_grant.
//  3. Refresh against the IdP and persist the rotated set.
//  4. On invalid_grant, re-read once more and delete the stored credential
//     ONLY if its refresh token still equals the one that failed. If a winning
//     process already stored a freshly-rotated token, we leave it intact
//     instead of clobbering it.
//
// ctxName/ref identify the stored credential; current is the token set the
// caller read before contending for the lock; issuer/clientID drive the
// refresh. ctx bounds the network calls.
//
// If the lock can't be acquired (e.g. an exotic filesystem), the refresh still
// proceeds lock-free — the equality-guarded delete keeps the destructive path
// safe even without the lock.
func (m *Manager) RefreshLocked(ctx context.Context, ctxName, ref, issuer, clientID string, current OIDCTokenSet) (OIDCTokenSet, error) {
	var unlock func()
	if m.credsPath != "" {
		lk := newFileLock(refreshLockPath(m.credsPath, ctxName))
		if err := lk.Lock(); err == nil {
			unlock = lk.Unlock
		}
		// Lock errors are non-fatal: fall through to a lock-free refresh.
	}
	if unlock != nil {
		defer unlock()

		// Re-read after acquiring the lock: a concurrent process may have
		// already rotated the token while we were blocked.
		if stored, err := m.GetByRef(ctxName, ref); err == nil && IsOIDCBlob(stored) {
			if ts, err := UnmarshalOIDC(stored); err == nil {
				if ts.Valid() {
					return ts, nil
				}
				// The stored refresh token may differ from ours if another
				// process refreshed but the new access token is itself already
				// near expiry; use the freshest refresh token we can see.
				current = ts
			}
		}
	}

	fresh, err := Refresh(ctx, issuer, clientID, current)
	if err != nil {
		if errors.Is(err, ErrRefreshFailed) {
			m.deleteIfRefreshTokenMatches(ctxName, ref, current.RefreshToken)
		}
		return OIDCTokenSet{}, err
	}

	blob, mErr := fresh.Marshal()
	if mErr != nil {
		return OIDCTokenSet{}, mErr
	}
	if _, sErr := m.Set(ctxName, blob); sErr != nil {
		return OIDCTokenSet{}, sErr
	}
	return fresh, nil
}

// deleteIfRefreshTokenMatches deletes the stored credential for ctxName only
// when the currently-stored refresh token still equals failedRefreshToken.
// This prevents a process that lost a rotation race from deleting the freshly
// rotated credential the winning process just stored. Best-effort: storage
// errors are swallowed (the caller already treats refresh failure as a
// re-login prompt).
func (m *Manager) deleteIfRefreshTokenMatches(ctxName, ref, failedRefreshToken string) {
	stored, err := m.GetByRef(ctxName, ref)
	if err != nil {
		// Already gone (or unreadable) — nothing to clobber.
		return
	}
	if IsOIDCBlob(stored) {
		if ts, err := UnmarshalOIDC(stored); err == nil && ts.RefreshToken != failedRefreshToken {
			// A different (rotated) refresh token is stored now — another
			// process won the race. Leave its credential intact.
			return
		}
	}
	_ = m.DeleteByRef(ctxName, ref)
}
