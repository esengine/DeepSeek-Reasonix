package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGrepSkipsVendorDirs proves a recursive grep prunes nested node_modules
// (perf + noise) but still searches one when it's the explicit target.
func TestGrepSkipsVendorDirs(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "app.go"), "the needle is here\n")
	mkfile(t, filepath.Join(dir, "node_modules", "dep", "lib.go"), "needle in a dependency\n")

	grepIn := func(path string) string {
		args, _ := json.Marshal(map[string]any{"pattern": "needle", "path": path})
		out, _ := grepTool{}.Execute(context.Background(), args)
		return out
	}

	root := grepIn(dir)
	if !strings.Contains(root, "app.go") {
		t.Errorf("grep should find app.go: %q", root)
	}
	if strings.Contains(root, "node_modules") {
		t.Errorf("grep should skip nested node_modules, got: %q", root)
	}

	nested := grepIn(filepath.Join(dir, "node_modules"))
	if !strings.Contains(nested, "lib.go") {
		t.Errorf("an explicit node_modules grep should still search it: %q", nested)
	}
}
