package auth

import (
	"errors"
	"testing"
)

// fakeStorage is an in-memory Storage used to drive Manager tests without
// touching the real OS keyring or filesystem.
type fakeStorage struct {
	kind   string
	tokens map[string]string
	// unavailable makes Set/Get/Delete return ErrKeyringUnavailable. Used to
	// exercise the keyring → plaintext fallback path.
	unavailable bool
	// tooSmall makes Set return ErrKeyringTooSmall, simulating macOS
	// Keychain's per-item size cap.
	tooSmall bool
}

func newFakeStorage(kind string) *fakeStorage {
	return &fakeStorage{kind: kind, tokens: map[string]string{}}
}

func (s *fakeStorage) Get(context string) (string, error) {
	if s.unavailable {
		return "", ErrKeyringUnavailable
	}
	v, ok := s.tokens[context]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *fakeStorage) Set(context, token string) error {
	if s.unavailable {
		return ErrKeyringUnavailable
	}
	if s.tooSmall {
		return ErrKeyringTooSmall
	}
	s.tokens[context] = token
	return nil
}

func (s *fakeStorage) Delete(context string) error {
	if s.unavailable {
		return ErrKeyringUnavailable
	}
	delete(s.tokens, context)
	return nil
}

func (s *fakeStorage) Kind() string { return s.kind }

func TestManagerSetPrefersKeyringByDefault(t *testing.T) {
	kr := newFakeStorage("keyring")
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "")

	ref, err := m.Set("prod", "tok-1")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ref != "keyring" {
		t.Fatalf("ref = %q; want keyring", ref)
	}
	if kr.tokens["prod"] != "tok-1" {
		t.Fatalf("keyring did not receive token: %+v", kr.tokens)
	}
	if _, stored := pt.tokens["prod"]; stored {
		t.Fatal("plaintext should not have been written to")
	}
}

func TestManagerSetFallsBackToPlaintextOnKeyringUnavailable(t *testing.T) {
	kr := newFakeStorage("keyring")
	kr.unavailable = true
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "")

	ref, err := m.Set("prod", "tok-2")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ref != "plaintext" {
		t.Fatalf("ref = %q; want plaintext", ref)
	}
	if pt.tokens["prod"] != "tok-2" {
		t.Fatalf("plaintext did not receive fallback: %+v", pt.tokens)
	}
}

func TestManagerSetFallsBackToPlaintextOnKeyringTooSmall(t *testing.T) {
	kr := newFakeStorage("keyring")
	kr.tooSmall = true
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "")

	ref, err := m.Set("prod", "value-too-big-for-keychain")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ref != "plaintext" {
		t.Fatalf("ref = %q; want plaintext", ref)
	}
	if pt.tokens["prod"] != "value-too-big-for-keychain" {
		t.Fatalf("plaintext did not receive fallback: %+v", pt.tokens)
	}
}

func TestManagerSetHonoursKeyringOnlyPolicy(t *testing.T) {
	kr := newFakeStorage("keyring")
	kr.unavailable = true
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "keyring")

	_, err := m.Set("prod", "tok-3")
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("want ErrKeyringUnavailable when policy=keyring and keyring down; got %v", err)
	}
	if _, stored := pt.tokens["prod"]; stored {
		t.Fatal("plaintext was used despite policy=keyring")
	}
}

func TestManagerSetHonoursPlaintextPolicy(t *testing.T) {
	kr := newFakeStorage("keyring")
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "plaintext")

	ref, err := m.Set("prod", "tok-4")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ref != "plaintext" {
		t.Fatalf("ref = %q; want plaintext", ref)
	}
	if _, used := kr.tokens["prod"]; used {
		t.Fatal("keyring was used despite policy=plaintext")
	}
}

func TestManagerGetByRef(t *testing.T) {
	kr := newFakeStorage("keyring")
	kr.tokens["prod"] = "from-keyring"
	pt := newFakeStorage("plaintext")
	pt.tokens["prod"] = "from-plaintext"
	m := newManagerWith(kr, pt, "")

	if tok, err := m.GetByRef("prod", "keyring"); err != nil || tok != "from-keyring" {
		t.Fatalf("GetByRef(keyring) = %q, %v", tok, err)
	}
	if tok, err := m.GetByRef("prod", "plaintext"); err != nil || tok != "from-plaintext" {
		t.Fatalf("GetByRef(plaintext) = %q, %v", tok, err)
	}
	if _, err := m.GetByRef("prod", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByRef(empty) should be ErrNotFound; got %v", err)
	}
	if _, err := m.GetByRef("prod", "bogus"); err == nil {
		t.Fatal("GetByRef(bogus) should error")
	}
}

// TestSetByRefIgnoresPolicy verifies MEDIUM-2's building block: SetByRef writes
// to the backend named by ref, regardless of the KUPE_STORAGE write policy that
// Set would apply.
func TestSetByRefIgnoresPolicy(t *testing.T) {
	kr := newFakeStorage("keyring")
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "keyring") // policy would route Set to keyring

	if err := m.SetByRef("prod", "plaintext", "tok"); err != nil {
		t.Fatalf("SetByRef: %v", err)
	}
	if pt.tokens["prod"] != "tok" {
		t.Fatalf("plaintext did not receive the token: %+v", pt.tokens)
	}
	if _, used := kr.tokens["prod"]; used {
		t.Fatal("keyring was written despite ref=plaintext")
	}
	if err := m.SetByRef("prod", "bogus", "tok"); err == nil {
		t.Fatal("SetByRef(bogus) should error")
	}
}

func TestManagerDeleteByRefIsIdempotent(t *testing.T) {
	kr := newFakeStorage("keyring")
	pt := newFakeStorage("plaintext")
	m := newManagerWith(kr, pt, "")

	if err := m.DeleteByRef("prod", "keyring"); err != nil {
		t.Fatalf("delete on empty keyring should be nil; got %v", err)
	}
	if err := m.DeleteByRef("prod", ""); err != nil {
		t.Fatalf("delete with empty ref should be no-op; got %v", err)
	}
}
