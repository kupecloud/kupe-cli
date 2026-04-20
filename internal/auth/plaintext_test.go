package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPlaintextStorageRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kupe", "credentials.yaml")
	s := NewPlaintextStorage(path)

	if err := s.Set("prod", "tok-prod"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("staging", "tok-staging"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// File must be mode 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o; want 0600", mode)
	}

	if tok, err := s.Get("prod"); err != nil || tok != "tok-prod" {
		t.Fatalf("Get(prod) = %q, %v", tok, err)
	}
	if tok, err := s.Get("staging"); err != nil || tok != "tok-staging" {
		t.Fatalf("Get(staging) = %q, %v", tok, err)
	}
}

func TestPlaintextStorageGetMissing(t *testing.T) {
	s := NewPlaintextStorage(filepath.Join(t.TempDir(), "credentials.yaml"))
	_, err := s.Get("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v; want ErrNotFound", err)
	}
}

func TestPlaintextStorageDeleteIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	s := NewPlaintextStorage(path)
	if err := s.Delete("never-set"); err != nil {
		t.Fatalf("Delete on empty file returned error: %v", err)
	}
	if err := s.Set("x", "y"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete, Get = %v; want ErrNotFound", err)
	}
}
