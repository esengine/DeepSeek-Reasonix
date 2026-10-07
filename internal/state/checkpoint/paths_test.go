package checkpoint

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestValidateWorkspacePathRejectsSymlinkWithoutWorkspace(t *testing.T) {
	dir := testenv.TempDir(t)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(testenv.TempDir(t), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := validateWorkspacePath("", filepath.Join(link, "new.txt"))
	if !errors.Is(err, errSymlinkPath) {
		t.Fatalf("unrooted symlink path accepted: %v", err)
	}
}
