package repair

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestInspectInvalidProjectConfigIsReadOnlyByDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{Root: root, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) != 2 || report.Checks[1].Valid {
		t.Fatalf("checks = %+v", report.Checks)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("project config was modified without IncludeProject: %v", err)
	}
}

func TestInspectCanQuarantineInvalidProjectConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{Root: root, Apply: true, IncludeProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks[1].Exists || !report.Checks[1].Valid {
		t.Fatalf("project check after repair = %+v", report.Checks[1])
	}
	if matches, _ := filepath.Glob(path + ".reasonix-quarantine-*"); len(matches) != 1 {
		t.Fatalf("quarantine matches = %v", matches)
	}
}

func TestRepairRestoresLastKnownGoodGlobalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	original := []byte("default_model = \"deepseek-flash\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored config = %q, want %q", got, original)
	}
	if len(report.Applied) != 2 {
		t.Fatalf("applied = %v", report.Applied)
	}
}

func TestConfigSnapshotsRotateAndVerifyHash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	for i := 0; i < configSnapshotRetention+2; i++ {
		content := []byte("default_model = \"model-" + string(rune('a'+i)) + "\"\n")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RecordHealthyConfig("v1"); err != nil {
			t.Fatal(err)
		}
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != configSnapshotRetention {
		t.Fatalf("snapshots = %d, want %d", len(snapshots), configSnapshotRetention)
	}
	if err := os.WriteFile(snapshots[0].Path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("tampered snapshot was restored")
	}
}

func TestUndoRepairRestoresQuarantinedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	bad := []byte("[broken\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Fatalf("undone config = %q", got)
	}
}

func TestConfigRepairCommitsWhenAuditLogFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := config.UserConfigPath()
	bad := []byte("[broken\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repairLogPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true}); err != nil {
		t.Fatalf("repair must commit despite a failing audit log: %v", err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatalf("undo after audit-log failure: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(bad) {
		t.Fatalf("undone config = %q (%v), want %q", got, err, bad)
	}
}

func TestUndoRejectsTamperedRepairTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	previous := filepath.Join(home, "unrelated.previous")
	if err := os.WriteFile(previous, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = append(tx.Changes, RepairChange{Scope: "global", TargetPath: filepath.Join(t.TempDir(), "arbitrary.txt"), PreviousPath: previous})
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err == nil {
		t.Fatal("tampered repair transaction was accepted")
	}
}

func TestSnapshotUndoAcrossSeparateStateHome(t *testing.T) {
	home := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", stateHome)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"before\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}
	current := []byte("default_model = \"current\"\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(current) {
		t.Fatalf("undo restored %q, want %q", got, current)
	}
}

// TestRestoreConfigSnapshotPreservesSymlinkThroughUndo pins the dotfiles
// contract: restoring a snapshot over a symlinked config materializes the
// snapshot as a plain file (without writing through the link), and undo
// brings back the original symlink node itself.
func TestRestoreConfigSnapshotPreservesSymlinkThroughUndo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	dotfiles := filepath.Join(t.TempDir(), "dotfiles-config.toml")
	if err := os.WriteFile(dotfiles, []byte("default_model = \"good\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, dest); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}
	if err := os.WriteFile(dotfiles, []byte("default_model = \"drifted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("restore should materialize the snapshot as a plain file")
	}
	if got, _ := os.ReadFile(dest); string(got) != "default_model = \"good\"\n" {
		t.Fatalf("restored config = %q", got)
	}
	if got, _ := os.ReadFile(dotfiles); string(got) != "default_model = \"drifted\"\n" {
		t.Fatalf("restore wrote through the symlink: %q", got)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("undo materialized a regular file, want symlink (mode %v)", info.Mode())
	}
	if got, err := os.Readlink(dest); err != nil || got != dotfiles {
		t.Fatalf("restored link target = %q (%v), want %q", got, err, dotfiles)
	}
	if got, _ := os.ReadFile(dest); string(got) != "default_model = \"drifted\"\n" {
		t.Fatalf("config through restored link = %q", got)
	}
}

// TestRestoreConfigSnapshotCrossDeviceCleanupKeepsPlainConfig pins the
// fail-safe contract when the state dir sits on another filesystem: the
// backup is a byte copy (rename fails), and when the transaction save fails
// the cleanup must restore dest without a window where it is missing — a
// bare cross-device rename would fail and silently drop config.toml.
func TestRestoreConfigSnapshotCrossDeviceCleanupKeepsPlainConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	original := []byte("default_model = \"original\"\n")
	if err := os.WriteFile(dest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}

	originalRename := snapshotRename
	snapshotRename = func(string, string) error { return errors.New("injected cross-device rename") }
	t.Cleanup(func() { snapshotRename = originalRename })
	// Make saveRepairTransaction fail: its atomic write cannot replace a
	// directory squatting on the transaction path.
	if err := os.MkdirAll(repairTransactionPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("restore with failing transaction save should error")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("config.toml must survive the failed restore: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("config after cleanup = %q, want %q", got, original)
	}
}

// TestRestoreConfigSnapshotCrossDeviceCleanupRestoresSymlink is the symlink
// variant: the cross-device backup keeps only a recreated link node, and the
// failure cleanup must rebuild that link at dest instead of retrying the
// impossible rename and leaving dest as the half-restored plain file.
func TestRestoreConfigSnapshotCrossDeviceCleanupRestoresSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	dotfiles := filepath.Join(t.TempDir(), "dotfiles-config.toml")
	if err := os.WriteFile(dotfiles, []byte("default_model = \"linked\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, dest); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}

	originalRename := snapshotRename
	snapshotRename = func(string, string) error { return errors.New("injected cross-device rename") }
	t.Cleanup(func() { snapshotRename = originalRename })
	if err := os.MkdirAll(repairTransactionPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("restore with failing transaction save should error")
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("config.toml must survive the failed restore: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("cleanup materialized a regular file, want symlink (mode %v)", info.Mode())
	}
	if got, err := os.Readlink(dest); err != nil || got != dotfiles {
		t.Fatalf("restored link target = %q (%v), want %q", got, err, dotfiles)
	}
	if got, _ := os.ReadFile(dotfiles); string(got) != "default_model = \"linked\"\n" {
		t.Fatalf("dotfiles content = %q, must be untouched", got)
	}
}

// TestRestoreConfigSnapshotCommitsWhenAuditLogFails pins the commit boundary:
// last-repair.json is the durable undo state; the append-only audit log is
// best-effort. A failing audit append must not roll the restore back — that
// would consume the backup the just-persisted transaction points to, wedging
// every later UndoLastRepair — and undo itself must still succeed while the
// log stays unwritable.
func TestRestoreConfigSnapshotCommitsWhenAuditLogFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	original := []byte("default_model = \"original\"\n")
	if err := os.WriteFile(dest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}
	current := []byte("default_model = \"current\"\n")
	if err := os.WriteFile(dest, current, 0o600); err != nil {
		t.Fatal(err)
	}
	// Wedge the append-only audit log: a directory squatting on its path makes
	// appendRepairLog fail while last-repair.json still persists fine.
	if err := os.MkdirAll(repairLogPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatalf("restore must commit despite a failing audit log: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(original) {
		t.Fatalf("restored config = %q, want %q", got, original)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatalf("undo after audit-log failure: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(current) {
		t.Fatalf("undone config = %q, want %q", got, current)
	}
}
