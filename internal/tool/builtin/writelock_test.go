package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeLockFileFindsSiblingLock(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "~$report.docx"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := officeLockFile(doc)
	if !ok || got != "~$report.docx" {
		t.Fatalf("officeLockFile(%q) = %q, %v; want ~$report.docx, true", doc, got, ok)
	}
}

func TestOfficeLockFileCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "~$REPORT.DOCX"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := officeLockFile(doc); !ok {
		t.Fatalf("officeLockFile(%q) should match ~$REPORT.DOCX case-insensitively", doc)
	}
}

func TestOfficeLockFileNone(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := officeLockFile(doc); ok {
		t.Fatalf("officeLockFile(%q) matched without any lock file", doc)
	}
}

func TestOfficeLockFileSkipsLockTargets(t *testing.T) {
	// Writing a lock file itself must never match: the write target already
	// carries the ~$ prefix, so officeLockFile must not self-match.
	dir := t.TempDir()
	lock := filepath.Join(dir, "~$report.docx")
	if err := os.WriteFile(lock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := officeLockFile(lock); ok {
		t.Fatalf("officeLockFile(%q) self-matched a ~$ target", lock)
	}
}

func TestOfficeLockFileIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory named like the lock file must not be treated as a lock.
	if err := os.Mkdir(filepath.Join(dir, "~$report.docx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := officeLockFile(doc); ok {
		t.Fatalf("officeLockFile(%q) matched a directory", doc)
	}
}

func TestAnnotateWriteLockErrorWithOfficeLock(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "~$report.docx"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := errors.New("rename report.docx: access denied")
	err := annotateWriteLockError(doc, base)
	if !errors.Is(err, base) {
		t.Fatalf("annotateWriteLockError must preserve the wrapped error: %v", err)
	}
	for _, want := range []string{"locked by an Office app", "~$report.docx", "retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("annotated error should mention %q: %v", want, err)
		}
	}
}

func TestAnnotateWriteLockErrorUnrelatedError(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := errors.New("no space left on device")
	err := annotateWriteLockError(doc, base)
	if err != base {
		t.Fatalf("unrelated errors must pass through unchanged, got: %v", err)
	}
}
