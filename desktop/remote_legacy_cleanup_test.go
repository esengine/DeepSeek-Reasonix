package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

func seedLegacyWorkbenchData(t *testing.T, trust bool) {
	t.Helper()
	dir := config.MemoryUserDir()
	if dir == "" {
		t.Fatal("test MemoryUserDir is empty")
	}
	if err := os.MkdirAll(filepath.Join(dir, "remote-mirrors", "host-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, "remote-mirrors", "host-a", name), make([]byte, 128), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if trust {
		if err := os.WriteFile(filepath.Join(dir, "remote-provider-trust.json"), []byte(`{"v":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanRemoteLegacyWorkbenchDataReadsCountsOnly(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	seedLegacyWorkbenchData(t, true)
	a := &App{}
	view := a.ScanRemoteLegacyWorkbenchData()
	if view.MirrorCount != 2 {
		t.Fatalf("mirror count = %d, want 2", view.MirrorCount)
	}
	if view.MirrorBytes != 256 {
		t.Fatalf("mirror bytes = %d, want 256", view.MirrorBytes)
	}
	if !view.TrustFile {
		t.Fatal("trust file not detected")
	}

	a2 := &App{}
	view2 := a2.ScanRemoteLegacyWorkbenchData()
	if view2.MirrorCount != 2 || !view2.TrustFile {
		t.Fatalf("scan mutated the data it reads: rescan = %+v", view2)
	}
}

func TestScanRemoteLegacyWorkbenchDataEmptyWhenAbsent(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	a := &App{}
	view := a.ScanRemoteLegacyWorkbenchData()
	if view.MirrorCount != 0 || view.MirrorBytes != 0 || view.TrustFile {
		t.Fatalf("empty scan = %+v", view)
	}
}

func TestCleanRemoteLegacyWorkbenchDataRemovesMirrorsOnly(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	seedLegacyWorkbenchData(t, true)
	a := &App{}
	if err := a.CleanRemoteLegacyWorkbenchData("mirrors"); err != nil {
		t.Fatal(err)
	}
	view := a.ScanRemoteLegacyWorkbenchData()
	if view.MirrorCount != 0 {
		t.Fatalf("mirrors survived cleanup: %+v", view)
	}
	if !view.TrustFile {
		t.Fatal("trust file removed by the mirrors cleanup")
	}
	if err := a.CleanRemoteLegacyWorkbenchData("trust"); err != nil {
		t.Fatal(err)
	}
	if a.ScanRemoteLegacyWorkbenchData().TrustFile {
		t.Fatal("trust file survived cleanup")
	}
}

func TestCleanRemoteLegacyWorkbenchDataRejectsUnknownAndEscapeTargets(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	a := &App{}
	if err := a.CleanRemoteLegacyWorkbenchData("everything"); err == nil {
		t.Fatal("unknown target accepted")
	}
	if err := a.CleanRemoteLegacyWorkbenchData("../remote-mirrors"); err == nil {
		t.Fatal("path traversal target accepted")
	}
	if err := a.CleanRemoteLegacyWorkbenchData(""); err == nil {
		t.Fatal("empty target accepted")
	}
}

func TestCleanRemoteLegacyWorkbenchDataRejectsSymlinkedMirrorDir(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	if os.Getenv("REASONIX_LEGACY_CLEANUP_NO_SYMLINK") != "" {
		t.Skip("environment does not allow symlinks")
	}
	dir := config.MemoryUserDir()
	if dir == "" {
		t.Fatal("test MemoryUserDir is empty")
	}
	outside := t.TempDir()
	link := filepath.Join(dir, "remote-mirrors")
	_ = os.RemoveAll(link)
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	a := &App{}
	if err := a.CleanRemoteLegacyWorkbenchData("mirrors"); err == nil {
		t.Fatal("symlinked mirror dir was accepted for cleanup")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

func TestCleanRemoteLegacyWorkbenchDataIdempotentWhenAbsent(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	a := &App{}
	if err := a.CleanRemoteLegacyWorkbenchData("mirrors"); err != nil {
		t.Fatalf("cleanup of absent mirrors = %v", err)
	}
	if err := a.CleanRemoteLegacyWorkbenchData("trust"); err != nil {
		t.Fatalf("cleanup of absent trust = %v", err)
	}
}

func TestWithinReasonixPrivateDirRejectsEscapes(t *testing.T) {

	t.Setenv("REASONIX_STATE_HOME", t.TempDir())
	dir := config.MemoryUserDir()
	for _, path := range []string{
		filepath.Join(dir, "remote-mirrors"),
		filepath.Join(dir, "nested", "remote-provider-trust.json"),
	} {
		if !withinReasonixPrivateDir(path) {
			t.Fatalf("legitimate path rejected: %q", path)
		}
	}
	for _, path := range []string{
		filepath.Join(dir, "..", "elsewhere"),
		filepath.Join(dir, ".."),
		filepath.Join(dir, "remote-mirrors", "..", "..", "escape"),
		"/etc/passwd",
	} {
		if withinReasonixPrivateDir(path) {
			t.Fatalf("escape path accepted: %q", path)
		}
	}
}
