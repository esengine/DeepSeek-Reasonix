//go:build windows

package builtin

import (
	"context"
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
	for _, want := range []string{"in use by another process", "retry"} {
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
	if !strings.Contains(err.Error(), "in use by another process") {
		t.Errorf("lock violation should be annotated, got: %v", err)
	}
}

// TestAnnotateWriteLockErrorAccessDeniedRename covers the errno the atomic
// rename actually produces against a holder with FILE_SHARE_READ only: a plain
// ERROR_ACCESS_DENIED inside *os.LinkError, not ERROR_SHARING_VIOLATION.
func TestAnnotateWriteLockErrorAccessDeniedRename(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := &os.LinkError{Op: "rename", Old: "tmp", New: doc, Err: windows.ERROR_ACCESS_DENIED}
	err := annotateWriteLockError(doc, base)
	if !errors.Is(err, base) {
		t.Fatalf("annotateWriteLockError must preserve the wrapped error: %v", err)
	}
	if !strings.Contains(err.Error(), "in use by another process") {
		t.Errorf("rename access denied should be annotated, got: %v", err)
	}
}

// TestAnnotateWriteLockErrorAccessDeniedPathError covers the negative case:
// an ERROR_ACCESS_DENIED from a non-rename step (*os.PathError, e.g. a
// directory the caller cannot enter) is not a lock symptom and must pass
// through unchanged.
func TestAnnotateWriteLockErrorAccessDeniedPathError(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := &os.PathError{Op: "mkdir", Path: doc, Err: windows.ERROR_ACCESS_DENIED}
	err := annotateWriteLockError(doc, base)
	if err != base {
		t.Fatalf("non-rename access denied must pass through unchanged, got: %v", err)
	}
}

// holdDenyWrite opens path with FILE_SHARE_READ only — the exact sharing mode
// Microsoft Word uses while a document is open: readers may share, but no other
// process may write or delete the file, so the atomic rename must fail.
func holdDenyWrite(t *testing.T, path string) func() {
	t.Helper()
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(pathPtr, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Skipf("cannot hold a deny-write handle: %v", err)
	}
	return func() { windows.CloseHandle(handle) }
}

// TestWriteFileLockedByOfficeAppLock is the end-to-end regression test for
// issue #5599: a document held open like Word holds it (deny-write handle plus
// a ~$ sibling lock file) must fail with an actionable hint naming the lock
// file, and must leave the document untouched.
func TestWriteFileLockedByOfficeAppLock(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "~$report.docx"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := holdDenyWrite(t, doc)
	defer release()

	_, err := writeFile{}.Execute(context.Background(), argsJSON(t, map[string]any{"path": doc, "content": "new"}))
	if err == nil {
		t.Fatal("expected the locked write to fail")
	}
	for _, want := range []string{"locked by an Office app", "~$report.docx", "retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
	got, _ := os.ReadFile(doc)
	if string(got) != "hello" {
		t.Fatalf("locked write must leave the document unchanged, got %q", got)
	}
}

// TestWriteFileLockedByOtherProcess covers a lock without an Office lock file:
// the generic sharing-violation hint must still explain the failure.
func TestWriteFileLockedByOtherProcess(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(doc, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := holdDenyWrite(t, doc)
	defer release()

	_, err := writeFile{}.Execute(context.Background(), argsJSON(t, map[string]any{"path": doc, "content": "new"}))
	if err == nil {
		t.Fatal("expected the locked write to fail")
	}
	if !strings.Contains(err.Error(), "in use by another process") {
		t.Errorf("error should carry the generic lock hint: %v", err)
	}
	got, _ := os.ReadFile(doc)
	if string(got) != "old" {
		t.Fatalf("locked write must leave the file unchanged, got %q", got)
	}
}
