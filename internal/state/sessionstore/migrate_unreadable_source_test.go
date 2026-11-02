package sessionstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

const unreadableSourceSession = `{"role":"user","content":"arrives on the retry"}
{"role":"assistant","content":"well, eventually"}
`

// unreadableSourceDir makes dir exist but not be listable, and returns the skip
// class the import should report plus a restore func that leaves dir empty and
// readable. A chmod-000 directory is the real-world failure; where that does not
// deny reads (Windows, or a root test user) a regular file at the same path
// makes os.ReadDir fail with ENOTDIR where the platform reports it.
func unreadableSourceDir(t *testing.T, dir string) (SkipReason, func()) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(dir); err != nil {
		return SkipPermission, func() { _ = os.Chmod(dir, 0o755) }
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Windows reports a non-directory as absent, which is the normal fresh
	// install, so no unreadable directory can be built there.
	if _, err := os.ReadDir(dir); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Skip("this platform cannot make a directory unreadable")
	}
	return SkipCorrupt, func() {
		_ = os.Remove(dir)
		_ = os.MkdirAll(dir, 0o755)
	}
}

// An unreadable source is not an empty one: the implicit import must return the
// typed read failure instead of (0, nil), and it must not stamp the one-time
// markers as if every session had arrived. Once the source is readable again the
// retry imports the session and only then stamps them.
func TestMigrateLegacySessionsReportsAnUnreadableSourceAndWithholdsTheMarker(t *testing.T) {
	root := testenv.TempDir(t)
	src := filepath.Join(root, "sessions")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	wantReason, restore := unreadableSourceDir(t, src)

	n, err := MigrateLegacySessions(src, dest, nil)
	if n != 0 {
		t.Fatalf("imported %d sessions from a source it could not read", n)
	}
	var unreadable *LegacySourceError
	if !errors.As(err, &unreadable) {
		t.Fatalf("an unreadable source must be reported, not read as empty: n=%d err=%v", n, err)
	}
	if unreadable.Path != src {
		t.Fatalf("read failure names %q, want the source %q", unreadable.Path, src)
	}
	if unreadable.Reason != wantReason {
		t.Fatalf("read failure classified %q, want %q", unreadable.Reason, wantReason)
	}
	if !strings.Contains(err.Error(), src) {
		t.Fatalf("read failure %q does not name the directory %q", err, src)
	}
	for _, marker := range []string{legacyRoutedHomeImportMarker, legacyJsonlPassMarker} {
		if importMarkerExists(dest, marker) {
			t.Fatalf("%s stamped although the source was never read, so the sessions are never retried", marker)
		}
	}

	// Repaired source: the next boot imports and only now records completion.
	restore()
	if err := os.WriteFile(filepath.Join(src, "chat-1.jsonl"), []byte(unreadableSourceSession), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err = MigrateLegacySessions(src, dest, nil)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n != 1 {
		t.Fatalf("retry imported %d sessions, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "chat-1.jsonl")); err != nil {
		t.Fatalf("retry did not import the session: %v", err)
	}
	for _, marker := range []string{legacyRoutedHomeImportMarker, legacyJsonlPassMarker} {
		if !importMarkerExists(dest, marker) {
			t.Fatalf("a successful retry left %s unwritten, so every later boot would import again", marker)
		}
	}
}

// Control: the explicit --from path already reports a source it cannot list in
// its typed report, and its caller consumes that report, not an error. Bringing
// the implicit path up to it must leave this contract alone.
func TestImportLegacySessionsFromExplicitDirStillReportsAnUnreadableSource(t *testing.T) {
	root := testenv.TempDir(t)
	src := filepath.Join(root, "sessions")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	wantReason, restore := unreadableSourceDir(t, src)
	defer restore()

	n, rep, err := ImportLegacySessionsFromExplicitDir(src, dest, nil)
	if err != nil {
		t.Fatalf("explicit import reports the source in its report, not as an error: %v", err)
	}
	if n != 0 {
		t.Fatalf("imported %d sessions from a source it could not read", n)
	}
	if len(rep.Skipped) != 1 {
		t.Fatalf("skips = %+v, want the unreadable source directory", rep.Skipped)
	}
	skip := rep.Skipped[0]
	if skip.Path != src || skip.Name != filepath.Base(src) || skip.Reason != wantReason {
		t.Fatalf("skip = %+v, want path=%s name=%s reason=%s", skip, src, filepath.Base(src), wantReason)
	}
}
