//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeLocalOpenPathPreservesUnixRoot(t *testing.T) {
	got, err := normalizeLocalOpenPath("file:///tmp/reasonix-report.md")
	if err != nil {
		t.Fatalf("Unix file URL rejected: %v", err)
	}
	if got != "/tmp/reasonix-report.md" {
		t.Fatalf("Unix file URL = %q, want rooted path", got)
	}
}

func TestOpenLocalPathRejectsExecutableMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clicked-document")
	if err := os.WriteFile(path, []byte("not safe to launch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&App{}).OpenLocalPath(path); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("OpenLocalPath(executable mode) err = %v, want executable refusal", err)
	}
}

func TestOpenLocalPathRejectsSymlinkToApplicationBundle(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Unsafe.app")
	if err := os.Mkdir(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "innocent-folder")
	if err := os.Symlink(bundle, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{alias, alias + string(filepath.Separator), alias + string(filepath.Separator) + "."} {
		if err := (&App{}).OpenLocalPath(path); err == nil || !strings.Contains(err.Error(), "executable") {
			t.Errorf("OpenLocalPath(application symlink %q) err = %v, want executable refusal", path, err)
		}
	}
}

func TestOpenTargetPathAllowsSymlinkToDocument(t *testing.T) {
	dir := t.TempDir()
	document := filepath.Join(dir, "report.md")
	if err := os.WriteFile(document, []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "report-link")
	if err := os.Symlink(document, alias); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(alias)
	if err != nil {
		t.Fatal(err)
	}
	if !openTargetPathAllowed(alias, info) {
		t.Fatal("symlink to ordinary document should be openable")
	}
}
