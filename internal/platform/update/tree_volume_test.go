package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

var errNotSameDevice = errors.New("the system cannot move the file to a different disk drive")

// onTwoVolumes makes every path under other a second volume: a rename that
// crosses between it and the rest fails the way Windows fails one from C: to D:.
func onTwoVolumes(t *testing.T, other string) {
	t.Helper()
	restore := renameFile
	t.Cleanup(func() { renameFile = restore })
	onOther := func(p string) bool { return strings.HasPrefix(filepath.Clean(p), filepath.Clean(other)) }
	renameFile = func(from, to string) error {
		if onOther(from) != onOther(to) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errNotSameDevice}
		}
		return restore(from, to)
	}
}

// An install on another drive than the update cache can still take a swap:
// its backup lives inside the install, which is always the install's volume.
func TestTreeSwapBacksUpOnTheInstallVolume(t *testing.T) {
	base := testenv.TempDir(t)
	install, cache := filepath.Join(base, "install"), filepath.Join(base, "cache")
	put(t, install, "app.exe", "old app")
	onTwoVolumes(t, cache)

	if err := CheckTreeSwap(install, filepath.Join(cache, "delta")); !errors.Is(err, ErrTreeNotSwappable) {
		t.Fatalf("a backup on the cache's volume: err = %v, want ErrTreeNotSwappable", err)
	}
	if err := CheckTreeSwap(install, SwapBackupDir(install)); err != nil {
		t.Fatalf("a backup inside the install: %v", err)
	}
}

// Staged files on the cache's volume are copied in; only the backup has to
// be a rename, and it stays on the install's volume.
func TestApplyTreeCopiesStagingFromAnotherVolume(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host", "resources/new.js": "added"})
	h.BackupDir = filepath.Join(SwapBackupDir(h.InstallDir), h.Version)
	onTwoVolumes(t, h.StagingDir)

	if err := ApplyTree(h); err != nil {
		t.Fatalf("ApplyTree: %v", err)
	}
	for rel, want := range map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host", "resources/new.js": "added", "Uninstall Reasonix Studio.exe": "uninstaller"} {
		if got := read(t, h.InstallDir, rel); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
	if got := read(t, h.BackupDir, "resources/bin/host.exe"); got != "old host" {
		t.Fatalf("backup of host.exe = %q", got)
	}
}

// A swap that fails midway still restores every file when the staging tree is
// on another volume: the rollback renames only within the install's volume.
func TestApplyTreeRollsBackAcrossVolumes(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app", "resources/bin/host.exe": "new host", "resources/blocked/x.js": "new"})
	h.BackupDir = filepath.Join(SwapBackupDir(h.InstallDir), h.Version)
	put(t, h.InstallDir, "resources/blocked", "a file, not a directory")
	onTwoVolumes(t, h.StagingDir)

	if err := ApplyTree(h); err == nil {
		t.Fatal("a swap that could not install a file reported success")
	}
	for rel, want := range map[string]string{"app.exe": "old app", "resources/bin/host.exe": "old host"} {
		if got := read(t, h.InstallDir, rel); got != want {
			t.Fatalf("after rollback %s = %q, want %q", rel, got, want)
		}
	}
}

// A file copied in from another volume lands whole or not at all: it is
// written under a temporary name beside its destination and renamed there,
// so a crash mid-copy never leaves a half-written file at the install path.
func TestACrossVolumeCopyIsRenamedIntoPlaceWhole(t *testing.T) {
	quickRetries(t)
	h := handoff(t, map[string]string{"app.exe": "new app", "resources/new.js": "added"})
	h.BackupDir = filepath.Join(SwapBackupDir(h.InstallDir), h.Version)
	onTwoVolumes(t, h.StagingDir)
	crossing := renameFile
	var landed []string
	renameFile = func(from, to string) error {
		if strings.HasPrefix(filepath.Base(from), "."+filepath.Base(to)+".reasonix-part-") {
			if _, err := os.Lstat(to); !os.IsNotExist(err) {
				t.Errorf("%s existed before its copy was complete", to)
			}
			b, _ := os.ReadFile(from)
			landed = append(landed, filepath.Base(to)+"="+string(b))
		}
		return crossing(from, to)
	}
	if err := ApplyTree(h); err != nil {
		t.Fatalf("ApplyTree: %v", err)
	}
	if len(landed) != 2 || !strings.Contains(strings.Join(landed, ","), "app.exe=new app") {
		t.Fatalf("renamed into place: %q, want both files whole", landed)
	}
	if left, _ := filepath.Glob(filepath.Join(h.InstallDir, "*reasonix-part-*")); len(left) != 0 {
		t.Fatalf("temporary copies left behind: %v", left)
	}
}

// A copy that cannot be renamed into place leaves neither a destination nor
// its temporary copy.
func TestAFailedCrossVolumeCopyLeavesNothing(t *testing.T) {
	base := testenv.TempDir(t)
	src, dst := filepath.Join(base, "staged", "app.exe"), filepath.Join(base, "install", "app.exe")
	put(t, filepath.Dir(src), "app.exe", "new app")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	restore := renameFile
	t.Cleanup(func() { renameFile = restore })
	renameFile = func(string, string) error { return errNotSameDevice }
	if err := moveFile(src, dst); err == nil {
		t.Fatal("moveFile reported success without placing the file")
	}
	if entries, _ := os.ReadDir(filepath.Dir(dst)); len(entries) != 0 {
		t.Fatalf("install dir holds %d entries, want none", len(entries))
	}
}

// An install whose directory refuses a new subdirectory (an all-users install
// under Program Files) cannot take a swap, and says so as ErrTreeNotSwappable
// rather than as a disk failure.
func TestABackupDirTheInstallRefusesIsNotSwappable(t *testing.T) {
	install := testenv.TempDir(t)
	restore := mkdirAll
	t.Cleanup(func() { mkdirAll = restore })
	mkdirAll = func(string, os.FileMode) error { return os.ErrPermission }
	if err := CheckTreeSwap(install, SwapBackupDir(install)); !errors.Is(err, ErrTreeNotSwappable) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err = %v, want ErrTreeNotSwappable wrapping the refusal", err)
	}
}
