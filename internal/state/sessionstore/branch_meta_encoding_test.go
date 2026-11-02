package sessionstore

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The sidecar is JSON this package writes, so it is UTF-8 by construction. One
// stray byte must not send the whole file through a legacy charset, where a
// lead byte swallows the backslash of `\\W` and leaves an invalid escape.
func TestLoadBranchMetaKeepsUTF8WithStrayByte(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "s.jsonl")
	raw := "{\"id\":\"s\",\"workspace_root\":\"D:\\\\测试目\\\\Workspace\",\"preview\":\"x\x80\"}"
	if err := os.WriteFile(BranchMetaPath(path), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if want := `D:\测试目\Workspace`; meta.WorkspaceRoot != want {
		t.Fatalf("WorkspaceRoot = %q, want %q", meta.WorkspaceRoot, want)
	}
}
