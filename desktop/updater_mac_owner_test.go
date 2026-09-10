//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/repair"
)

func TestMacUpdateHandoffParserRequiresOwnerPID(t *testing.T) {
	base := []string{
		"-to-version", "v2",
		"-created-at", "2026-07-28T00:00:00Z",
		"-transaction-id", strings.Repeat("a", 64),
	}
	if _, err := parseMacUpdateHandoffArgs(base); err == nil || !strings.Contains(err.Error(), "missing required") {
		t.Fatalf("handoff without -owner-pid parsed: %v", err)
	}
	cfg, err := parseMacUpdateHandoffArgs(append(base, "-owner-pid", "4242"))
	if err != nil || cfg.OwnerPID != 4242 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestMacUpdateHandoffRejectsOwnerPIDMismatchBeforeTouchingBundle(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	pending := filepath.Join(root, "pending.json")
	logPath := filepath.Join(root, "update.log")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         oldApp + ".reasonix-update-backup",
		HandoffAppPath:     newApp,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, logPath, nil)
	cfg := macHandoffConfigFor(tx)
	cfg.OwnerPID = 12345
	if code := runMacUpdateHandoff(cfg); code == 0 {
		t.Fatal("handoff accepted a wait pid that differs from the prepared transaction")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "old" {
		t.Fatalf("installed bundle changed: %q, %v", got, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("pending transaction was cleared on identity mismatch: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(logData), "does not match handoff identity") {
		t.Fatalf("handoff log = %q err=%v", logData, err)
	}
}
