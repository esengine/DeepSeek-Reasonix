package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestForkFileViewsPreservesDisabledProtection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	reg.Add(readFile{workDir: root})
	reg.Add(writeFile{workDir: root})
	bind := ForkFileViews(reg)
	read, _ := reg.Get("read_file")
	write, _ := reg.Get("write_file")
	if err := runViewTool(t, bind(read), map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, bind(write), map[string]string{"path": path, "content": "unprotected\n"}); err != nil {
		t.Fatalf("disabled protection became mandatory: %v", err)
	}
}
