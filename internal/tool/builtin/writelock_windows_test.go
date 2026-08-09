//go:build windows

package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAnnotateWriteLockErrorSharingViolation(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The atomic rename wraps the raw errno in *os.LinkError; errors.Is must
	// unwrap it to recognize the sharing violation (WinError 32).
	base := &os.LinkError{Op: "rename", Old: "tmp", New: doc, Err: windows.ERROR_SHARING_VIOLATION}
	err := annotateWriteLockError(doc, base)
	if !errors.Is(err, base) {
		t.Fatalf("annotateWriteLockError must preserve the wrapped error: %v", err)
	}
	for _, want := range []string{"locked by another process", "retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("annotated error should mention %q: %v", want, err)
		}
	}
}

func TestAnnotateWriteLockErrorLockViolation(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := &os.LinkError{Op: "rename", Old: "tmp", New: doc, Err: windows.ERROR_LOCK_VIOLATION}
	err := annotateWriteLockError(doc, base)
	if !errors.Is(err, base) {
		t.Fatalf("annotateWriteLockError must preserve the wrapped error: %v", err)
	}
	if !strings.Contains(err.Error(), "locked by another process") {
		t.Errorf("lock violation should be annotated, got: %v", err)
	}
}
