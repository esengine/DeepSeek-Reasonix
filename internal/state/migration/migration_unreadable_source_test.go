package migration

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessionstore"
)

// unreadableSessionDir makes a legacy sessions directory that exists but cannot
// be listed, and returns the skip class the import should report for it plus a
// restore func that leaves dir as an empty, readable directory. It prefers the
// real-world failure — a chmod-000 directory — and falls back to a regular file
// at the same path, which makes os.ReadDir fail with ENOTDIR on Unix.
func unreadableSessionDir(t *testing.T, dir string) (sessionstore.SkipReason, func()) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(dir); err != nil {
		return sessionstore.SkipPermission, func() { _ = os.Chmod(dir, 0o755) }
	}
	// chmod 000 did not deny listing here (Windows, or a root test user). A
	// regular file at the same path is the portable construction.
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
	return sessionstore.SkipCorrupt, func() {
		_ = os.Remove(dir)
		_ = os.MkdirAll(dir, 0o755)
	}
}

// A sessions directory that exists but cannot be listed is not an empty one.
// Before the fix /migrate printed "no legacy sessions needed migration" and
// returned success, so a user with an unreadable legacy dir was told there was
// nothing to migrate and never fixed the access.
func TestRunLegacyRescueNamesTheUnreadableSessionsDirectory(t *testing.T) {
	home := migrationRescueHome(t)
	legacyDir := filepath.Join(home, ".reasonix", "sessions")
	unreadableSessionDir(t, legacyDir)

	var notices []string
	res := RunLegacyRescue(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	joined := strings.Join(notices, "\n")

	if strings.Contains(joined, "no legacy sessions needed migration") {
		t.Fatalf("an unreadable sessions directory was reported as nothing to migrate:\n%s", joined)
	}
	if strings.Contains(joined, "no legacy data needed migration") {
		t.Fatalf("an unreadable sessions directory was summarized as no legacy data:\n%s", joined)
	}
	for _, want := range []string{"could not read legacy sessions directory", legacyDir} {
		if !strings.Contains(joined, want) {
			t.Fatalf("rescue did not name the unreadable directory (%q missing) in:\n%s", want, joined)
		}
	}

	var unreadable *sessionstore.LegacySourceError
	reported := false
	for _, err := range res.SessionErrs {
		if errors.As(err, &unreadable) && unreadable.Path == legacyDir {
			reported = true
			break
		}
	}
	if !reported {
		t.Fatalf("rescue did not record a typed read failure for %s: %v", legacyDir, res.SessionErrs)
	}
}

// Control: an empty but readable directory is still "nothing to migrate", with
// no error. The fix must not turn a genuinely empty source into a warning.
func TestRunLegacyRescueTreatsAnEmptyReadableSessionsDirAsNothingToMigrate(t *testing.T) {
	home := migrationRescueHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".reasonix", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	var notices []string
	res := RunLegacyRescue(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	}))
	if len(res.SessionErrs) != 0 {
		t.Fatalf("an empty source produced session errors: %v", res.SessionErrs)
	}
	if len(res.SessionSkips) != 0 {
		t.Fatalf("an empty source produced session skips: %+v", res.SessionSkips)
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "migration rescue: no legacy sessions needed migration") {
		t.Fatalf("an empty readable source must still say there is nothing to migrate:\n%s", joined)
	}
}

// Control: boot calls migration.MigrateLegacySessionSources (assembly_reuse.go,
// migrateLegacySources) and that call is deliberately quiet unless sessions
// arrived. A source it cannot read must not start printing at startup: the
// notice belongs to the explicit /migrate rescue, not to every launch.
func TestBootSessionImportStaysQuietWhenTheSourceCannotBeRead(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_HOME", "")
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Chdir(testenv.TempDir(t))
	legacyDir := filepath.Join(home, ".reasonix", "sessions")
	unreadableSessionDir(t, legacyDir)

	var notices []event.Event
	imports := MigrateLegacySessionSources(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	}))
	if len(imports) != 0 {
		t.Fatalf("a source that cannot be read must import nothing: %+v", imports)
	}
	if len(notices) != 0 {
		t.Fatalf("a boot-time import that cannot read %s must stay quiet, got %+v", legacyDir, notices)
	}
}
