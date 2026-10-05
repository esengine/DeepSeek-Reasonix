#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
# Delete has to reuse the package's existing sentinel, not invent an error.
cat > zz_authoritative_test.go <<'GO'
package store

import (
	"errors"
	"testing"
)

func TestAuthoritativeDelete(t *testing.T) {
	s := New()
	if err := s.Put("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatalf("delete present: %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("len = %d, want 0", s.Len())
	}
	if _, err := s.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}
}
GO
go test ./... >/dev/null
grep -q "Delete" store_test.go || { echo "no test was added for Delete"; exit 1; }
