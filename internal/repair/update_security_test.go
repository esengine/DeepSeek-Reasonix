package repair

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPendingUpdateRejectsTargetOutsideGuardInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	guardDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "reasonix-desktop")
	backup := filepath.Join(home, "repair", "updates", "reasonix-desktop.previous")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx := &UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		TargetKind:    "file",
		TargetPath:    target,
		BackupPath:    backup,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(guardDir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if _, err := ReadPendingUpdate(); err == nil {
		t.Fatal("pending update outside Guard install was accepted")
	}
}

func TestInstalledUpdateStateRejectsSymlinkedParentEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx, err := PrepareFileUpdate("v1", "v2", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	pendingBefore, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}

	updatesDir := filepath.Dir(tx.BackupPath)
	outside := filepath.Join(t.TempDir(), "moved-updates")
	if err := os.Rename(updatesDir, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, updatesDir); err != nil {
		t.Fatal(err)
	}
	sidecarOutside := filepath.Join(outside, filepath.Base(installedFileUpdateStatePath(tx)))
	record := &installedFileUpdateState{
		SchemaVersion:       1,
		UpdateTransactionID: UpdateTransactionID(tx),
		InstalledStateIDs:   []string{repairPlanReleaseNodeState(target)},
	}

	if err := createInstalledFileUpdateState(tx, record); err == nil ||
		!strings.Contains(err.Error(), "resolves outside the repair directory") {
		t.Fatalf("record installed state through parent symlink = %v", err)
	}
	if _, err := os.Lstat(sidecarOutside); !os.IsNotExist(err) {
		t.Fatalf("sidecar escaped the repair directory: %v", err)
	}
	if pendingAfter, err := os.ReadFile(PendingUpdatePath()); err != nil ||
		string(pendingAfter) != string(pendingBefore) {
		t.Fatalf("rejected sidecar write changed pending recovery state: %q, %v", pendingAfter, err)
	}
}

func TestPendingUpdateRejectsUnexpectedReleaseFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	backup := filepath.Join(home, "repair", "updates", "reasonix-desktop.previous")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	const hash = "deadbeef"
	bad := []UpdateTransactionFile{
		{TargetPath: filepath.Join(dir, "evil.exe"), BackupPath: backup, SHA256: hash},
		{TargetPath: filepath.Join(t.TempDir(), "reasonix-guard"), BackupPath: backup, SHA256: hash},
		{TargetPath: filepath.Join(dir, "reasonix-guard"), BackupPath: filepath.Join(t.TempDir(), "loose.previous"), SHA256: hash},
		{TargetPath: filepath.Join(dir, "reasonix-guard"), BackupPath: backup}, // missing hash
		{TargetPath: filepath.Join(dir, "reasonix-guard"), BackupPath: backup, SHA256: hash, MissingBefore: true},
		{TargetPath: target, MissingBefore: true},
	}
	for _, file := range bad {
		tx := &UpdateTransaction{
			SchemaVersion: 1,
			ToVersion:     "v2",
			Platform:      runtime.GOOS + "/" + runtime.GOARCH,
			TargetKind:    "file",
			TargetPath:    target,
			BackupPath:    backup,
			BackupSHA256:  hash,
			Files:         []UpdateTransactionFile{{TargetPath: target, BackupPath: backup, SHA256: hash}, file},
			CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := overwritePendingUpdateForTest(tx); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPendingUpdate(); err == nil {
			t.Fatalf("release file entry %+v was accepted", file)
		}
	}
}

func TestPendingUpdateRejectsBackupSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	repairDir := filepath.Join(home, "repair")
	if err := os.MkdirAll(repairDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repairDir, "updates")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err == nil {
		t.Fatal("prepare update wrote a backup through a symlink outside the repair directory")
	}
	backup := filepath.Join(repairDir, "updates", "reasonix-desktop.previous")
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	tx := &UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		ToVersion:     "v2",
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		TargetKind:    "file",
		TargetPath:    target,
		BackupPath:    backup,
		BackupSHA256:  "deadbeef",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingUpdate(); err == nil {
		t.Fatal("pending update accepted a backup that resolves outside the repair directory")
	}
}

func TestPrepareFileUpdateRejectsSymlinkReleaseFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-binary")
	if err := os.WriteFile(outside, []byte("outside"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err == nil {
		t.Fatal("prepare update accepted a symlinked release executable")
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("rejected release symlink was modified: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "outside" {
		t.Fatalf("rejected release symlink referent changed: %q, %v", got, err)
	}
}

func TestCopyFileWithHashRejectsSymlinkSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source")
	if err := os.Symlink(outside, source); err != nil {
		t.Fatal(err)
	}
	if _, err := copyFileWithHashCreate(source, filepath.Join(dir, "backup"), 0o600); err == nil {
		t.Fatal("copyFileWithHashCreate followed a symlink source")
	}
	if _, err := os.Lstat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
		t.Fatalf("symlink source created a backup: %v", err)
	}
}

func TestRenameRepairNodeNoReplacePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := renameRepairNodeNoReplace(source, destination); err == nil {
		t.Fatal("no-replace rename overwrote an existing destination")
	}
	for path, want := range map[string]string{source: "source", destination: "destination"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
		}
	}
}

func TestPendingUpdateAcceptsMissingReleaseSibling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	backup := filepath.Join(home, "repair", "updates", "reasonix-desktop.previous")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	tx := &UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		TargetKind:    "file",
		TargetPath:    target,
		BackupPath:    backup,
		BackupSHA256:  "deadbeef",
		Files: []UpdateTransactionFile{
			{TargetPath: target, BackupPath: backup, SHA256: "deadbeef"},
			{TargetPath: filepath.Join(dir, "Reasonix.exe"), MissingBefore: true},
		},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("valid missing release sibling was rejected: %v", err)
	}
}

func TestPendingUpdateAcceptsWindowsReleaseUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-launcher.exe"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	names := []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-launcher.exe",
		"reasonix-update-helper.exe",
		"reasonix-cli.exe",
		"Reasonix.exe",
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o700); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if _, err := PrepareFileUpdate("v1", "v2", paths[0], paths[1:]...); err != nil {
		t.Fatalf("prepare Windows release unit: %v", err)
	}
	tx, err := ReadPendingUpdate()
	if err != nil {
		t.Fatalf("read Windows release unit: %v", err)
	}
	if len(tx.Files) != len(names) {
		t.Fatalf("release unit files = %d, want %d: %+v", len(tx.Files), len(names), tx.Files)
	}
	for i, file := range tx.Files {
		if got := filepath.Base(file.TargetPath); got != names[i] {
			t.Fatalf("release unit file %d = %q, want %q", i, got, names[i])
		}
	}
}

func TestPendingUpdateAcceptsLinuxReleaseUnit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	names := []string{"reasonix-desktop", "reasonix-guard", "reasonix"}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o700); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if _, err := PrepareFileUpdate("v1", "v2", paths[0], paths[1:]...); err != nil {
		t.Fatalf("prepare Linux release unit: %v", err)
	}
	tx, err := ReadPendingUpdate()
	if err != nil {
		t.Fatalf("read Linux release unit: %v", err)
	}
	if len(tx.Files) != len(names) {
		t.Fatalf("release unit files = %d, want %d: %+v", len(tx.Files), len(names), tx.Files)
	}
}

func TestPendingUpdateRejectsHashlessOrPrimaryLessTransactions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	backup := filepath.Join(home, "repair", "updates", "reasonix-desktop.previous")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	guardBackup := filepath.Join(home, "repair", "updates", "reasonix-guard.previous")
	txs := map[string]*UpdateTransaction{
		"missing primary hash": {
			SchemaVersion: 1, ToVersion: "v2", TargetKind: "file",
			TargetPath: target, BackupPath: backup,
			Platform: runtime.GOOS + "/" + runtime.GOARCH, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		"release unit omits primary executable": {
			SchemaVersion: 1, ToVersion: "v2", TargetKind: "file",
			TargetPath: target, BackupPath: backup, BackupSHA256: "deadbeef",
			Files:    []UpdateTransactionFile{{TargetPath: filepath.Join(dir, "reasonix-guard"), BackupPath: guardBackup, SHA256: "deadbeef"}},
			Platform: runtime.GOOS + "/" + runtime.GOARCH, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	for name, tx := range txs {
		if err := overwritePendingUpdateForTest(tx); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPendingUpdate(); err == nil {
			t.Fatalf("%s: transaction was accepted", name)
		}
	}
}

func TestPendingUpdateRejectsPortableAliasAsPrimaryTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "Reasonix.exe")
	guard := filepath.Join(dir, "reasonix-guard.exe")
	backup := filepath.Join(home, "repair", "updates", "Reasonix.exe.previous")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("launcher"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return guard, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	tx := &UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		ToVersion:     "v2",
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		TargetKind:    "file",
		TargetPath:    target,
		BackupPath:    backup,
		BackupSHA256:  "deadbeef",
		Files: []UpdateTransactionFile{{
			TargetPath: target,
			BackupPath: backup,
			SHA256:     "deadbeef",
		}},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPendingUpdate(); err == nil ||
		!strings.Contains(err.Error(), "not a Reasonix executable") {
		t.Fatalf("portable alias was accepted as primary target: %v", err)
	}
}

func TestPendingUpdateRejectsIncompleteOrInconsistentIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*UpdateTransaction)
		want   string
	}{
		{
			name: "missing platform",
			mutate: func(tx *UpdateTransaction) {
				tx.Platform = ""
			},
			want: "transaction identity is incomplete",
		},
		{
			name: "invalid creation identity",
			mutate: func(tx *UpdateTransaction) {
				tx.CreatedAt = "not-a-timestamp"
			},
			want: "creation identity is invalid",
		},
		{
			name: "primary backup path mismatch",
			mutate: func(tx *UpdateTransaction) {
				tx.BackupPath = tx.Files[1].BackupPath
			},
			want: "primary backup metadata is inconsistent",
		},
		{
			name: "primary backup hash mismatch",
			mutate: func(tx *UpdateTransaction) {
				tx.BackupSHA256 = "deadbeef"
			},
			want: "primary backup metadata is inconsistent",
		},
		{
			name: "duplicate backup path",
			mutate: func(tx *UpdateTransaction) {
				tx.Files[1].BackupPath = tx.Files[0].BackupPath
			},
			want: "duplicate release backup",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", t.TempDir())
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "reasonix-desktop")
			guard := filepath.Join(dir, "reasonix-guard")
			originalExecutable := repairExecutable
			repairExecutable = func() (string, error) { return guard, nil }
			t.Cleanup(func() { repairExecutable = originalExecutable })
			for path, body := range map[string]string{target: "old-desktop", guard: "old-guard"} {
				if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := PrepareFileUpdate("v1", "v2", target, guard)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(tx)
			if err := overwritePendingUpdateForTest(tx); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadPendingUpdate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("read error = %v, want %q", err, tc.want)
			}
		})
	}
}
