//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"reasonix/internal/repair"
)

func TestMacUpdateHandoffPublishesPayloadDigestAcrossModeChange(t *testing.T) {
	probeA := filepath.Join(t.TempDir(), "marker")
	probeB := filepath.Join(t.TempDir(), "marker")
	for _, path := range []string{probeA, probeB} {
		if err := os.WriteFile(path, []byte("probe"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(probeB, 0o700); err != nil {
		t.Fatal(err)
	}
	requireDistinctPOSIXMode(t, probeA, probeB)

	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	for _, dir := range []string{oldApp, newApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldApp, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newApp, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := repair.AppBundlePayloadTreeDigest(newApp)
	if err != nil {
		t.Fatal(err)
	}
	strict, err := repair.AppBundleTreeDigest(newApp)
	if err != nil {
		t.Fatal(err)
	}
	if payload == strict {
		t.Fatal("payload digest collapsed onto the strict digest")
	}
	tx := &repair.UpdateTransaction{
		ToVersion:          "v2",
		CreatedAt:          "2026-07-28T00:00:00Z",
		TargetKind:         "app-bundle",
		TargetPath:         oldApp,
		BackupPath:         backupApp,
		HandoffAppPath:     newApp,
		HandoffAppTreeID:   payload,
		HandoffStagingPath: filepath.Dir(newApp),
		HandoffOwnerPID:    99999999,
	}
	installMacHandoffTestDeps(t, tx, pending, filepath.Join(root, "update.log"), nil)
	originalCopy := macHandoffCopy
	macHandoffCopy = func(oldPath, newPath string) error {
		if err := originalCopy(oldPath, newPath); err != nil {
			return err
		}
		copied := filepath.Join(newPath, "marker")
		return os.Chmod(copied, 0o700)
	}
	originalOpen := openCommand
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	t.Cleanup(func() {
		macHandoffCopy = originalCopy
		openCommand = originalOpen
	})

	if code := runMacUpdateHandoff(macHandoffConfigFor(tx)); code != 0 {
		t.Fatal("handoff rejected a mode-only copy of a payload digest")
	}
	if got, err := os.ReadFile(filepath.Join(oldApp, "marker")); err != nil || string(got) != "new" {
		t.Fatalf("installed marker = %q, %v", got, err)
	}
}

func requireDistinctPOSIXMode(t *testing.T, a, b string) {
	t.Helper()
	modeA, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	modeB, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}
	if modeA.Mode() == modeB.Mode() {
		t.Skip("filesystem does not preserve POSIX mode bits")
	}
}

func TestMacUpdateRenameFallsBackWhenExclusiveUnsupported(t *testing.T) {
	for _, unsupported := range []error{syscall.ENOTSUP, syscall.ENOSYS} {
		t.Run(unsupported.Error(), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source")
			destination := filepath.Join(dir, "destination")
			if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := macRenameNoReplace(func(string, string) error { return unsupported }, source, destination); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(destination)
			if err != nil || string(got) != "payload" {
				t.Fatalf("destination = %q, %v", got, err)
			}
			if _, err := os.Lstat(source); !os.IsNotExist(err) {
				t.Fatalf("source still exists: %v", err)
			}
		})
	}
}

func TestMacUpdateRenameFallbackDoesNotReplaceExisting(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := macRenameNoReplace(func(string, string) error { return syscall.ENOTSUP }, source, destination); err == nil {
		t.Fatal("fallback renamed over an existing destination")
	} else if !errors.Is(err, os.ErrExist) || !strings.Contains(err.Error(), "best-effort under Reasonix mutation lock") {
		t.Fatalf("fallback err = %v, want ErrExist", err)
	}
	for path, want := range map[string]string{source: "source", destination: "destination"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
		}
	}
}

func TestMacUpdateRenameDoesNotFallbackOnOtherErrors(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := macRenameNoReplace(func(string, string) error { return syscall.EPERM }, source, destination)
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("err = %v, want EPERM", err)
	}
	got, readErr := os.ReadFile(source)
	if readErr != nil || string(got) != "payload" {
		t.Fatalf("source = %q, %v", got, readErr)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("destination created after non-fallback error: %v", statErr)
	}
}
