//go:build darwin || linux

package skill

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginWarningsSkipNamedPipes(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "agents", "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"agents/pipe.md", "agents/directory/SKILL.md"} {
		if err := syscall.Mkfifo(filepath.Join(root, filepath.FromSlash(path)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg := pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Agents: []string{"agents"}}}
	if warnings := PluginWarnings(pkg); len(warnings) != 0 {
		t.Fatalf("pipe warnings=%v", warnings)
	}
}
