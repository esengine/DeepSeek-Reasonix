package serve

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A Studio launched with a PATH that lacks %SystemRoot% still has to find the
// file manager, which lives there and nowhere else.
func TestFolderCommandFindsExplorerWithoutPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("explorer.exe is a Windows file manager")
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		t.Skip("SystemRoot is not set on this machine")
	}
	t.Setenv("PATH", t.TempDir())

	cmd := folderCommand(t.TempDir())
	if cmd.Err != nil {
		t.Fatalf("folderCommand resolved no executable: %v", cmd.Err)
	}
	if want := filepath.Join(root, "explorer.exe"); !strings.EqualFold(cmd.Path, want) {
		t.Fatalf("folderCommand path = %q, want %q", cmd.Path, want)
	}
}
