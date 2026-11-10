package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestTargetAccessLeavesAWriteBelowAFileToTheTool(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "Program.cs/x.cs", "content": "y"})
	w := Workspace{Dir: dir}
	if err := w.TargetAccessCheck()(context.Background(), writeFile{workDir: dir, roots: []string{dir}}, args); err != nil {
		t.Fatalf("access check = %v; a path below a regular file is no access verdict, the write itself fails and stays an attempted write", err)
	}
}
