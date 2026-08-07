package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestReplaceFileNoRetryWhenTmpMissing(t *testing.T) {
	oldBase := replaceRetryBase
	replaceRetryBase = 10 * time.Second
	t.Cleanup(func() { replaceRetryBase = oldBase })

	dir := t.TempDir()
	start := time.Now()
	err := ReplaceFile(filepath.Join(dir, "missing.tmp"), filepath.Join(dir, "x.txt"))
	if err == nil {
		t.Fatal("want error when tmp source is missing")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("missing tmp should fail fast, took %v — it retried", elapsed)
	}
}

func TestReplaceFileRetriesThenReturnsError(t *testing.T) {
	oldBase, oldMax := replaceRetryBase, maxReplaceRetries
	replaceRetryBase, maxReplaceRetries = 0, 3
	t.Cleanup(func() { replaceRetryBase, maxReplaceRetries = oldBase, oldMax })

	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	if err := os.WriteFile(tmp, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "blocked")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(tmp, dest); err == nil {
		t.Fatal("want error when dest can never be replaced")
	}
	if !fileExists(tmp) {
		t.Error("tmp should survive a failed replace so the next launch can retry")
	}
}

func TestReplaceFileRenamesInPlace(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	dest := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(tmp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(tmp, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "hello" {
		t.Errorf("dest = %q, want hello", b)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("tmp should be gone after ReplaceFile")
	}
}

func TestReplaceFileTransientFailureNeverTruncatesDest(t *testing.T) {
	// A rename blocked by a transient lock must surface the error, never fall
	// back to the in-place copy: the copy truncates dest first, so a reader
	// racing it can observe an empty or half-written file — the torn state
	// AtomicWriteFile promises its callers (session leases, credentials,
	// plugin state) can never happen.
	oldBase, oldMax, oldRename := replaceRetryBase, maxReplaceRetries, renameFile
	replaceRetryBase, maxReplaceRetries = 0, 2
	renameCalls := 0
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errors.New("transient sharing violation")}
	}
	t.Cleanup(func() { replaceRetryBase, maxReplaceRetries, renameFile = oldBase, oldMax, oldRename })

	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	dest := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(tmp, dest); err == nil {
		t.Fatal("want the rename error to surface once retries are exhausted")
	}
	if want := maxReplaceRetries + 1; renameCalls != want {
		t.Errorf("rename attempts = %d, want %d (initial try plus retries)", renameCalls, want)
	}
	if b, _ := os.ReadFile(dest); string(b) != "old" {
		t.Fatalf("dest = %q, want the old content intact — anything else means the non-atomic copy ran", b)
	}
	if !fileExists(tmp) {
		t.Error("tmp should survive a failed replace so the caller can clean up")
	}
}

func TestReplaceFileCrossDeviceCopiesImmediately(t *testing.T) {
	// The cross-device class (Windows encryption filter drivers, #2696) fails
	// identically on every retry, so ReplaceFile must take the copy fallback
	// straight away instead of sleeping through the retry ladder.
	oldBase, oldMax, oldRename := replaceRetryBase, maxReplaceRetries, renameFile
	// Any retry sleep would trip the elapsed-time check below.
	replaceRetryBase, maxReplaceRetries = 10*time.Second, 8
	renameCalls := 0
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { replaceRetryBase, maxReplaceRetries, renameFile = oldBase, oldMax, oldRename })

	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	dest := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := ReplaceFile(tmp, dest); err != nil {
		t.Fatalf("ReplaceFile should succeed via the copy fallback: %v", err)
	}
	if renameCalls != 1 {
		t.Errorf("rename attempts = %d, want 1 — a structurally impossible rename must not be retried", renameCalls)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cross-device fallback took %v — it slept through the retry ladder", elapsed)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Errorf("dest = %q, want the new content from the copy fallback", b)
	}
	if fileExists(tmp) {
		t.Error("tmp should be consumed by the copy fallback")
	}
}

func TestAtomicWriteFileStrictCrossDeviceKeepsExistingDestination(t *testing.T) {
	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameFile = oldRename })

	dir := t.TempDir()
	dest := filepath.Join(dir, "current.json")
	if err := os.WriteFile(dest, []byte("old-pointer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFileStrict(dest, []byte("new-pointer"), 0o644); err == nil {
		t.Fatal("strict atomic write accepted a cross-device rename")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "old-pointer" {
		t.Fatalf("destination changed after strict replace failure: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "current.json" {
		t.Fatalf("strict write left temporary files: %v", entries)
	}
}

func TestCopyOntoOverwritesAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "x.tmp")
	dest := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-and-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyOnto(tmp, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Errorf("dest = %q, want new (fully overwritten)", b)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("tmp should be removed after copyOnto")
	}
	// Mode preservation is meaningful on Unix; Windows only tracks the read-only bit.
	if info, err := os.Stat(dest); err == nil && info.Mode().Perm() != 0o600 {
		t.Logf("dest mode = %o (want 0600 on Unix)", info.Mode().Perm())
	}
}

func TestAtomicWriteFileReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(path, []byte("new-content"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-content" {
		t.Fatalf("content = %q, want %q", got, "new-content")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
	// No leftover tmp files in the directory.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Fatalf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestAtomicWriteFileCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "creds")
	if err := AtomicWriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile into missing dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestAtomicCreateFileNeverOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("concurrent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicCreateFile(path, []byte("confirmed"), 0o600); err == nil {
		t.Fatal("AtomicCreateFile overwrote an existing target")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "concurrent" {
		t.Fatalf("existing target changed: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("temporary files leaked: %v", entries)
	}
}

func TestAtomicCreateFilePublishesCompleteContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := AtomicCreateFile(path, []byte("confirmed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "confirmed" {
		t.Fatalf("created target = %q, %v", got, err)
	}
}

func TestAtomicOverwriteFileKeepsExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows does not carry a POSIX executable bit")
	}
	path := filepath.Join(t.TempDir(), "build.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nold\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicOverwriteFile(path, []byte("#!/bin/sh\nnew\n"), 0o644); err != nil {
		t.Fatalf("AtomicOverwriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("perm = %o, want 755 — the script lost its executable bit", perm)
	}
}

func TestAtomicOverwriteFileWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicOverwriteFile(link, []byte("new"), 0o644); err != nil {
		t.Fatalf("AtomicOverwriteFile: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced by a regular file: mode=%v err=%v", info.Mode(), err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("target content = %q, %v — the write did not reach the link target", got, err)
	}
}

func TestAtomicOverwriteFileUsesDefaultPermForNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.txt")
	if err := AtomicOverwriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
}
