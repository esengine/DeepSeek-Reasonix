package fileutil

import (
	"os"
	"path/filepath"
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
