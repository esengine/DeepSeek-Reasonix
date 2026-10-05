package builtin

import (
	"path/filepath"
	"runtime"
	"testing"
)

// A workspace at a drive root splits to the bare volume, and every match the
// walk then yielded was drive-relative: "D:DeepSeek-Reasonix\..." names a path
// under whatever D:'s current directory happens to be, not under D:\.
func TestAGlobRootedAtADriveWalksTheDriveRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows has a per-drive current directory to be relative to")
	}
	root, rel := globWalkRoot(`D:\**\*xray*`)
	if want := `D:\`; root != want {
		t.Fatalf("root = %q, want %q — a bare volume names the drive's current directory", root, want)
	}
	if want := "**/*xray*"; rel != want {
		t.Fatalf("rel = %q, want %q", rel, want)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("root %q is not absolute, so every match under it is drive-relative", root)
	}
	// The workspace root itself is the same shape with nothing left to match.
	if root, rel := globWalkRoot(`D:\`); root != `D:\` || rel != "**" {
		t.Fatalf("a drive root split to %q, %q; want the root itself and a match-everything pattern", root, rel)
	}
}

// The repair applies to exactly one shape: a pattern whose root already names a
// directory reaches the walk unchanged.
func TestAGlobRootThatNamesADirectoryIsUntouched(t *testing.T) {
	dir := t.TempDir()
	root, rel := globWalkRoot(filepath.Join(dir, "**", "*.go"))
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
	if rel != "**/*.go" {
		t.Fatalf("rel = %q, want %q", rel, "**/*.go")
	}
}
