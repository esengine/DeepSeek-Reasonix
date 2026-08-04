package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveSupersededPendingFileUpdateMovesExactTransaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	setLegacyRepairExecutable(t, root)
	target := filepath.Join(root, "reasonix-desktop")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1.18.0", "v1.19.1", target); err != nil {
		t.Fatal(err)
	}
	archived, err := ArchiveSupersededPendingFileUpdate("v1.20.0", root)
	if err != nil || !archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	if _, err := ReadPendingUpdate(); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "repair", "legacy-updates"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive entries=%v err=%v", entries, err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "old" {
		t.Fatalf("target=%q err=%v", body, err)
	}
}

func TestArchiveSupersededPendingFileUpdateRejectsEqualVersion(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	setLegacyRepairExecutable(t, root)
	target := filepath.Join(root, "reasonix-desktop")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1.18.0", "v1.19.1", target); err != nil {
		t.Fatal(err)
	}
	if archived, err := ArchiveSupersededPendingFileUpdate("v1.19.1", root); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("rejected transaction was removed: %v", err)
	}
}

func TestArchiveSupersededPendingFileUpdateRejectsForeignInstall(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	owner := t.TempDir()
	setLegacyRepairExecutable(t, owner)
	target := filepath.Join(owner, "reasonix-desktop")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1.18.0", "v1.19.1", target); err != nil {
		t.Fatal(err)
	}
	if archived, err := ArchiveSupersededPendingFileUpdate("v1.20.0", t.TempDir()); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("foreign transaction was removed: %v", err)
	}
}

func setLegacyRepairExecutable(t *testing.T, root string) {
	t.Helper()
	original := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(root, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = original })
}
