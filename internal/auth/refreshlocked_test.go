package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// newPlaintextManager builds a Manager whose storage is a real plaintext file
// in a temp dir and whose refresh locks co-locate there. Forces plaintext
// policy so tests never touch the host keyring.
func newPlaintextManager(t *testing.T) *Manager {
	t.Helper()
	credsPath := t.TempDir() + "/credentials.yaml"
	return &Manager{
		keyring:   NewKeyringStorage(),
		plaintext: NewPlaintextStorage(credsPath),
		policy:    "plaintext",
		credsPath: credsPath,
	}
}

// TestRefreshLockedPersistsRotatedToken proves the happy path stores the
// rotated token set under the lock.
func TestRefreshLockedPersistsRotatedToken(t *testing.T) {
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	})
	defer srv.Close()

	m := newPlaintextManager(t)
	current := OIDCTokenSet{RefreshToken: "old-refresh", Expiry: time.Now().Add(-time.Minute)}
	blob, _ := current.Marshal()
	if _, err := m.Set("prod", blob); err != nil {
		t.Fatal(err)
	}

	fresh, err := m.RefreshLocked(context.Background(), "prod", "plaintext", issuer, "kupe-cli", current)
	if err != nil {
		t.Fatalf("RefreshLocked: %v", err)
	}
	if fresh.AccessToken != "new-access" || fresh.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected fresh token: %+v", fresh)
	}
	stored, err := m.GetByRef("prod", "plaintext")
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := UnmarshalOIDC(stored)
	if ts.RefreshToken != "new-refresh" {
		t.Fatalf("stored RefreshToken = %q; want new-refresh", ts.RefreshToken)
	}
}

// TestRefreshLockedSkipsWhenAnotherProcessAlreadyRefreshed simulates the race
// winner: before the loser refreshes, a fresh valid token is already on disk.
// RefreshLocked must return the stored fresh set WITHOUT calling the IdP.
func TestRefreshLockedSkipsWhenAlreadyFresh(t *testing.T) {
	called := false
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	m := newPlaintextManager(t)
	// On disk: a freshly rotated, still-valid token set (the winner's result).
	winner := OIDCTokenSet{AccessToken: "winner-access", RefreshToken: "winner-refresh", Expiry: time.Now().Add(time.Hour)}
	blob, _ := winner.Marshal()
	if _, err := m.Set("prod", blob); err != nil {
		t.Fatal(err)
	}

	// The loser still holds the OLD token set it read before contending.
	loserCurrent := OIDCTokenSet{AccessToken: "old-access", RefreshToken: "old-refresh", Expiry: time.Now().Add(-time.Minute)}
	got, err := m.RefreshLocked(context.Background(), "prod", "plaintext", issuer, "kupe-cli", loserCurrent)
	if err != nil {
		t.Fatalf("RefreshLocked: %v", err)
	}
	if called {
		t.Fatal("IdP was called even though a fresh token was already stored")
	}
	if got.AccessToken != "winner-access" {
		t.Fatalf("got %q; want winner-access (the already-stored fresh token)", got.AccessToken)
	}
}

// TestRefreshLockedDeletesOnInvalidGrantWhenTokenUnchanged covers the genuine
// expiry: invalid_grant and the stored refresh token still equals the failed
// one -> delete the credential.
func TestRefreshLockedDeletesOnInvalidGrant(t *testing.T) {
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	defer srv.Close()

	m := newPlaintextManager(t)
	current := OIDCTokenSet{RefreshToken: "dead-refresh", Expiry: time.Now().Add(-time.Minute)}
	blob, _ := current.Marshal()
	if _, err := m.Set("prod", blob); err != nil {
		t.Fatal(err)
	}

	_, err := m.RefreshLocked(context.Background(), "prod", "plaintext", issuer, "kupe-cli", current)
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("want ErrRefreshFailed, got %v", err)
	}
	if _, err := m.GetByRef("prod", "plaintext"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("credential should be deleted; GetByRef err = %v", err)
	}
}

// TestRefreshLockedDoesNotDeleteWinnersToken is the core KC-1 regression for
// the lock-free fallback path (credsPath empty => no file lock, mirroring a
// filesystem where flock is unavailable). The loser refreshes with its stale
// token and gets invalid_grant, but by the time it goes to delete, a DIFFERENT
// (rotated) refresh token is already stored. The equality guard must leave the
// winner's credential intact.
func TestRefreshLockedDoesNotDeleteWinnersToken(t *testing.T) {
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	defer srv.Close()

	m := newPlaintextManager(t)
	m.credsPath = "" // disable the file lock to exercise the equality guard alone

	// On disk: the WINNER's freshly rotated token (different refresh token).
	winner := OIDCTokenSet{AccessToken: "winner-access", RefreshToken: "winner-refresh", Expiry: time.Now().Add(time.Hour)}
	blob, _ := winner.Marshal()
	if _, err := m.Set("prod", blob); err != nil {
		t.Fatal(err)
	}

	// The loser refreshes with the OLD token it read before the winner rotated.
	loserCurrent := OIDCTokenSet{RefreshToken: "old-refresh", Expiry: time.Now().Add(-time.Minute)}
	_, err := m.RefreshLocked(context.Background(), "prod", "plaintext", issuer, "kupe-cli", loserCurrent)
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("want ErrRefreshFailed, got %v", err)
	}
	// The winner's credential must survive — deleteIfRefreshTokenMatches saw a
	// different refresh token and left it alone.
	stored, err := m.GetByRef("prod", "plaintext")
	if err != nil {
		t.Fatalf("winner credential was clobbered: %v", err)
	}
	ts, _ := UnmarshalOIDC(stored)
	if ts.RefreshToken != "winner-refresh" {
		t.Fatalf("stored RefreshToken = %q; want winner-refresh (intact)", ts.RefreshToken)
	}
}

// TestRefreshLockedConcurrentDoesNotClobber drives two concurrent
// RefreshLocked calls through the real file lock against a server that rotates
// once then rejects. Exactly one wins; the loser must not delete the winner's
// stored credential. After both return, a valid credential must remain.
func TestRefreshLockedConcurrentDoesNotClobber(t *testing.T) {
	var mu sync.Mutex
	served := false
	issuer, srv := fakeAuthentik(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		first := !served
		served = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if first {
			_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":3600}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	defer srv.Close()

	m := newPlaintextManager(t)
	current := OIDCTokenSet{RefreshToken: "r0", Expiry: time.Now().Add(-time.Minute)}
	blob, _ := current.Marshal()
	if _, err := m.Set("prod", blob); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = m.RefreshLocked(context.Background(), "prod", "plaintext", issuer, "kupe-cli", current)
		}()
	}
	wg.Wait()

	// A valid credential must survive regardless of who won.
	stored, err := m.GetByRef("prod", "plaintext")
	if err != nil {
		t.Fatalf("credential was clobbered by the race: %v", err)
	}
	ts, _ := UnmarshalOIDC(stored)
	if ts.RefreshToken != "r1" {
		t.Fatalf("stored RefreshToken = %q; want r1 (the rotated winner)", ts.RefreshToken)
	}
}
