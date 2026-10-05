package builtin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The system follows a link before it applies a `..`; the path checked has to
// be the path opened.
func TestRealPathFollowsALinkBeforeDotDot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows resolves .. before it opens")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(base, "other")
	if err := os.MkdirAll(filepath.Join(other, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "sub"), filepath.Join(root, "sublink")); err != nil {
		t.Skip(err)
	}
	got, err := realPath(root + "/sublink/../notes.txt")
	if err != nil || got != filepath.Join(other, "notes.txt") {
		t.Fatalf("realPath = %q, %v, want %q", got, err, filepath.Join(other, "notes.txt"))
	}
	if got, _ := realPath(root + "/sublink/../missing/deeper.txt"); got != filepath.Join(other, "missing", "deeper.txt") {
		t.Fatalf("a not-yet-existing tail = %q", got)
	}
	if !readOutsideScope([]string{root}, root+"/sublink/../notes.txt") {
		t.Fatal("a path that ends beside the workspace was inside its scope")
	}
}
