package auth

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// swapKeyringFns temporarily replaces the low-level keyring function vars and
// returns a restore func. Lets us simulate raw backend errors (e.g. a D-Bus
// connect failure on a headless Linux box) without a real Secret Service.
func swapKeyringFns(t *testing.T, get func(string, string) (string, error), set func(string, string, string) error, del func(string, string) error) {
	t.Helper()
	origGet, origSet, origDel := keyringGet, keyringSet, keyringDelete
	keyringGet, keyringSet, keyringDelete = get, set, del
	t.Cleanup(func() { keyringGet, keyringSet, keyringDelete = origGet, origSet, origDel })
}

// errDBus stands in for the raw, untyped errors zalando/go-keyring surfaces
// from the Secret Service provider on Linux when no secret service is running
// — it is NOT keyring.ErrUnsupportedPlatform.
var errDBus = errors.New("dbus: couldn't determine address of session bus")

func TestRealKeyringSetClassifiesRawErrorAsUnavailable(t *testing.T) {
	swapKeyringFns(t,
		func(string, string) (string, error) { return "", errDBus },
		func(string, string, string) error { return errDBus },
		func(string, string) error { return errDBus },
	)
	err := realKeyring{}.Set("svc", "ctx", "tok")
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Set raw D-Bus error: want ErrKeyringUnavailable, got %v", err)
	}
	if !errors.Is(err, errDBus) {
		t.Fatalf("Set should preserve the underlying cause; got %v", err)
	}
}

func TestRealKeyringSetTooBigStillClassifiedAsTooSmall(t *testing.T) {
	swapKeyringFns(t,
		keyring.Get,
		func(string, string, string) error { return keyring.ErrSetDataTooBig },
		keyring.Delete,
	)
	if err := (realKeyring{}).Set("svc", "ctx", "tok"); !errors.Is(err, ErrKeyringTooSmall) {
		t.Fatalf("Set too-big: want ErrKeyringTooSmall, got %v", err)
	}
}

func TestRealKeyringGetClassifiesRawErrorAsUnavailable(t *testing.T) {
	swapKeyringFns(t,
		func(string, string) (string, error) { return "", errDBus },
		keyring.Set,
		keyring.Delete,
	)
	if _, err := (realKeyring{}).Get("svc", "ctx"); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Get raw D-Bus error: want ErrKeyringUnavailable, got %v", err)
	}
}

func TestRealKeyringGetNotFoundPreserved(t *testing.T) {
	swapKeyringFns(t,
		func(string, string) (string, error) { return "", keyring.ErrNotFound },
		keyring.Set,
		keyring.Delete,
	)
	if _, err := (realKeyring{}).Get("svc", "ctx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get not-found: want ErrNotFound, got %v", err)
	}
}

// TestKeyringSetFallsBackThroughManager wires the real keyring (with a raw
// backend error) through the Manager's default policy and asserts the
// documented keyring→plaintext fallback fires — the headless-Linux scenario.
func TestKeyringSetFallsBackThroughManager(t *testing.T) {
	swapKeyringFns(t,
		func(string, string) (string, error) { return "", errDBus },
		func(string, string, string) error { return errDBus },
		func(string, string) error { return errDBus },
	)
	pt := newFakeStorage("plaintext")
	m := newManagerWith(NewKeyringStorage(), pt, "")
	ref, err := m.Set("prod", "tok")
	if err != nil {
		t.Fatalf("Set should fall back to plaintext on raw keyring error; got %v", err)
	}
	if ref != "plaintext" {
		t.Fatalf("ref = %q; want plaintext", ref)
	}
	if pt.tokens["prod"] != "tok" {
		t.Fatalf("plaintext did not receive fallback: %+v", pt.tokens)
	}
}
