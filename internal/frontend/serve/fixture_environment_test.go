package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateGitGlobalConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "global.gitconfig")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
}
