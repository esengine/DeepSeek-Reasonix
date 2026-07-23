package repair

import (
	"os"
	"path/filepath"
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
	tx := &UpdateTransaction{SchemaVersion: 1, ToVersion: "v2", TargetKind: "file", TargetPath: target, BackupPath: backup, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := WritePendingUpdate(tx); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(guardDir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if _, err := ReadPendingUpdate(); err == nil {
		t.Fatal("pending update outside Guard install was accepted")
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
			TargetKind:    "file",
			TargetPath:    target,
			BackupPath:    backup,
			BackupSHA256:  hash,
			Files:         []UpdateTransactionFile{{TargetPath: target, BackupPath: backup, SHA256: hash}, file},
			CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := WritePendingUpdate(tx); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPendingUpdate(); err == nil {
			t.Fatalf("release file entry %+v was accepted", file)
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
	if err := WritePendingUpdate(tx); err != nil {
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
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		"release unit omits primary executable": {
			SchemaVersion: 1, ToVersion: "v2", TargetKind: "file",
			TargetPath: target, BackupPath: backup, BackupSHA256: "deadbeef",
			Files:     []UpdateTransactionFile{{TargetPath: filepath.Join(dir, "reasonix-guard"), BackupPath: guardBackup, SHA256: "deadbeef"}},
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	for name, tx := range txs {
		if err := WritePendingUpdate(tx); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPendingUpdate(); err == nil {
			t.Fatalf("%s: transaction was accepted", name)
		}
	}
}
