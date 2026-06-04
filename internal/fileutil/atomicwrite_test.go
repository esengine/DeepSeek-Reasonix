package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceFileRenamesInPlace(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	dest := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(tmp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(tmp, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "hello" {
		t.Errorf("dest = %q, want hello", b)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("tmp should be gone after ReplaceFile")
	}
}

func TestCopyOntoOverwritesAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	dest := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-and-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyOnto(tmp, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Errorf("dest = %q, want new (fully overwritten)", b)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("tmp should be removed after copyOnto")
	}
	// Mode preservation is meaningful on Unix; Windows only tracks the read-only bit.
	if info, err := os.Stat(dest); err == nil && info.Mode().Perm() != 0o600 {
		t.Logf("dest mode = %o (want 0600 on Unix)", info.Mode().Perm())
	}
}

// TestReplaceFileBothFailJoinsErrors pins the behaviour that when os.Rename
// fails (the EXDEV / AV-locked-file case the fallback exists for) AND the
// copy-onto fallback also fails, the returned error carries BOTH reasons
// instead of silently dropping the second. The previous shape returned just
// the rename error, hiding why recovery also failed (e.g. tmp was deleted
// mid-rename, dest is read-only, disk full).
func TestReplaceFileBothFailJoinsErrors(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	// Source tmp is missing on purpose: os.Rename will fail with "no such
	// file or directory", AND the copy-onto fallback's ReadFile will fail
	// with the same root cause. Both errors should surface.
	dest := filepath.Join(dir, "x.txt")

	err := ReplaceFile(tmp, dest)
	if err == nil {
		t.Fatal("expected error when both rename and copy fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "rename") {
		t.Errorf("joined error should mention rename, got: %s", msg)
	}
	if !strings.Contains(msg, "copy fallback") {
		t.Errorf("joined error should mention copy fallback, got: %s", msg)
	}
	// errors.Join is unwrappable; both halves should be reachable through
	// Unwrap() for callers that want to switch on a specific cause.
	type unwrapper interface{ Unwrap() []error }
	if u, ok := err.(unwrapper); ok {
		parts := u.Unwrap()
		if len(parts) != 2 {
			t.Errorf("errors.Join should produce 2 parts, got %d", len(parts))
		}
	} else {
		t.Errorf("ReplaceFile error should implement Unwrap() []error, got %T", err)
	}
}

// TestCopyOntoSurvivesMissingTmp: copyOnto itself fails before WriteFile
// when the source is missing, and the error is the read error (not the
// stat error), which matches the "operator wants to know why recovery
// failed" intent.
func TestCopyOntoSurvivesMissingTmp(t *testing.T) {
	dir := t.TempDir()
	err := copyOnto(filepath.Join(dir, "no-such.tmp"), filepath.Join(dir, "dest.txt"))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist chain, got: %v", err)
	}
}

// TestContainsEmptyNeedleIsTrivial pins the contract of the local contains
// helper: an empty needle always matches. Used by isReadOnlyFS to short-
// circuit when the underlying OS error has no message at all.
func TestContainsEmptyNeedleIsTrivial(t *testing.T) {
	if !contains("anything", "") {
		t.Error("empty needle should always match")
	}
	if contains("", "x") {
		t.Error("empty haystack with non-empty needle should not match")
	}
	if !contains("hello world", "world") {
		t.Error("substring match should work")
	}
	if contains("hello", "World") {
		t.Error("case-sensitive: should not match")
	}
}
