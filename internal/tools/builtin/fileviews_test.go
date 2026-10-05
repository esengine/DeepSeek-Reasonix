package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func runViewTool(t *testing.T, tl interface {
	Execute(context.Context, json.RawMessage) (string, error)
}, args map[string]string) error {
	t.Helper()
	b, _ := json.Marshal(args)
	_, err := tl.Execute(context.Background(), b)
	return err
}

func TestFileViewsRefuseOnlyAnOverwriteOfSomethingElsesChange(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	views := NewFileViews()
	read := readFile{workDir: dir, views: views}
	edit := editFile{workDir: dir, views: views}
	write := writeFile{workDir: dir, views: views}

	if err := runViewTool(t, read, map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, edit, map[string]string{"path": path, "old_string": "one", "new_string": "two"}); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, write, map[string]string{"path": path, "content": "three\n"}); err != nil {
		t.Fatalf("the agent's own edit was taken for an outside change: %v", err)
	}

	if err := os.WriteFile(path, []byte("someone else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runViewTool(t, write, map[string]string{"path": path, "content": "four\n"})
	if !errors.Is(err, ErrFileChangedSinceSeen) {
		t.Fatalf("overwrite after an outside change: err = %v, want ErrFileChangedSinceSeen", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "someone else\n" {
		t.Fatalf("the outside change was lost: %q", got)
	}

	if err := runViewTool(t, read, map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, write, map[string]string{"path": path, "content": "five\n"}); err != nil {
		t.Fatalf("after reading it again the write should go through: %v", err)
	}
}

func TestFileViewsLeaveUnseenFilesAndNilViewsAlone(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, writeFile{workDir: dir, views: NewFileViews()}, map[string]string{"path": path, "content": "y\n"}); err != nil {
		t.Fatalf("a file the agent never saw holds nothing stale: %v", err)
	}
	if err := runViewTool(t, writeFile{workDir: dir}, map[string]string{"path": path, "content": "z\n"}); err != nil {
		t.Fatalf("with no views nothing is checked: %v", err)
	}
}
