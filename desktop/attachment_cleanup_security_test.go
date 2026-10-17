package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStaleAttachmentCleanupDoesNotFollowSwappedDirectory(t *testing.T) {
	a, root, _ := uploadFixture(t)
	outside := t.TempDir()
	attachments := filepath.Join(root, ".reasonix", "attachments")
	want := []byte("external victim must survive stale attachment cleanup")
	var victim string
	a.attachmentIOHook = func() {
		entries, err := os.ReadDir(attachments)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].IsDir() {
			t.Fatalf("saved attachment entries = %v", entries)
		}
		victim = filepath.Join(outside, entries[0].Name())
		if err := os.WriteFile(victim, want, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(attachments, filepath.Join(root, ".reasonix", "original-attachments")); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", attachments, outside).CombinedOutput(); err != nil {
				t.Fatalf("create attachment junction: %v: %s", err, out)
			}
		} else if err := os.Symlink(outside, attachments); err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		a.tabs["a"].SessionGeneration++
		a.mu.Unlock()
	}

	data := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte("saved APK fixture"))
	if ref, err := a.SavePastedFileForTab("a", "app.apk", data); err == nil || ref != "" {
		t.Fatalf("stale write returned ref %q, err %v", ref, err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("stale-owner cleanup deleted the external victim through the swapped attachment directory: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("external victim changed: %q", got)
	}
}
