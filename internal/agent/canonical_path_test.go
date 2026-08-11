package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalSessionPathMatchesLeaseRegistryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Mixed-Case", "20260705-Test.jsonl")
	if got, want := CanonicalSessionPath(path), canonicalSessionSavePath(path); got != want {
		t.Fatalf("CanonicalSessionPath(%q) = %q, want lease key %q", path, got, want)
	}
}

func TestCanonicalSessionPathResolvesDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realPath := filepath.Join(realDir, "session.jsonl")
	aliasPath := filepath.Join(aliasDir, "session.jsonl")
	if got, want := CanonicalSessionPath(aliasPath), CanonicalSessionPath(realPath); got != want {
		t.Fatalf("directory alias split session identity: %q != %q", got, want)
	}
}

func TestCanonicalSessionPathResolvesNearestExistingAncestor(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realPath := filepath.Join(realDir, "not-created", "nested", "session.jsonl")
	aliasPath := filepath.Join(aliasDir, "not-created", "nested", "session.jsonl")
	if got, want := CanonicalSessionPath(aliasPath), CanonicalSessionPath(realPath); got != want {
		t.Fatalf("nearest existing ancestor alias split session identity: %q != %q", got, want)
	}
}

func TestCanonicalSessionPathIdempotentAndEmptySafe(t *testing.T) {
	if got := CanonicalSessionPath(""); got != "" {
		t.Fatalf("empty path resolved to %q; must stay empty", got)
	}
	if got := CanonicalSessionPath("   "); got != "" {
		t.Fatalf("blank path resolved to %q; must stay empty", got)
	}
	path := filepath.Join(t.TempDir(), "A", "b.jsonl")
	key := CanonicalSessionPath(path)
	if again := CanonicalSessionPath(key); again != key {
		t.Fatalf("not idempotent: %q -> %q", key, again)
	}
}

func TestCanonicalSessionPathFoldsCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case folding is Windows-only")
	}
	path := filepath.Join(t.TempDir(), "Sessions", "20260705-Test.jsonl")
	if CanonicalSessionPath(path) != CanonicalSessionPath(strings.ToUpper(path)) {
		t.Fatal("case variants of one file produced distinct keys")
	}
}
