//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacUpdateScriptWaitsForExactProcessAndRollsBackLaunchFailure(t *testing.T) {
	root := t.TempDir()
	oldApp := filepath.Join(root, "Reasonix.app")
	newApp := filepath.Join(root, "staging", "Reasonix.app")
	backupApp := oldApp + ".reasonix-update-backup"
	pending := filepath.Join(root, "pending.json")
	logPath := filepath.Join(root, "update.log")
	binDir := filepath.Join(root, "bin")
	for _, dir := range []string{oldApp, newApp, binDir} {
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
	openStub := filepath.Join(binDir, "open")
	if err := os.WriteFile(openStub, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(root, "handoff.sh")
	body := macUpdateScript(oldApp, newApp, backupApp, pending, filepath.Dir(newApp), logPath, 99999999)
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
	if err := cmd.Run(); err == nil {
		t.Fatal("handoff should fail when LaunchServices rejects the replacement")
	}

	marker, err := os.ReadFile(filepath.Join(oldApp, "marker"))
	if err != nil {
		t.Fatalf("read restored marker: %v", err)
	}
	if string(marker) != "old" {
		t.Fatalf("restored marker = %q, want old", marker)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending transaction was not cleared: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "PID 99999999") || !strings.Contains(logText, "rolling back") {
		t.Fatalf("handoff log lacks PID/rollback diagnostics: %s", logText)
	}
}
