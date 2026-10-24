package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type changingReadOverlay struct {
	reads  int
	writes int
}

func TestPagedReadDoesNotCertifyUnseenFileContent(t *testing.T) {
	for _, page := range []string{`{"path":"a.txt","limit":1}`, `{"path":"a.txt","offset":99}`} {
		t.Run(page, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a.txt")
			if err := os.WriteFile(path, []byte("header\noriginal tail\n"), 0600); err != nil {
				t.Fatal(err)
			}
			views := NewStrictFileViews()
			reader := readFile{workDir: root, views: views}
			if _, err := reader.Execute(t.Context(), json.RawMessage(`{"path":"a.txt"}`)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("header\nother writer's tail\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.Execute(t.Context(), json.RawMessage(page)); err != nil {
				t.Fatal(err)
			}
			_, err := (writeFile{workDir: root, views: views}).Execute(t.Context(), json.RawMessage(`{"path":"a.txt","content":"edited header\noriginal tail\n"}`))
			if !errors.Is(err, ErrFileChangedSinceSeen) {
				t.Fatalf("paged read authorized stale overwrite: %v", err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != "header\nother writer's tail\n" {
				t.Fatalf("lost unseen content: %q", got)
			}
		})
	}
}

func (o *changingReadOverlay) ReadTextFile(context.Context, string) (string, bool) {
	o.reads++
	if o.reads == 1 {
		return "before\n", true
	}
	return "another writer\n", true
}
func (o *changingReadOverlay) WriteTextFile(context.Context, string, string) (bool, error) {
	o.writes++
	return true, nil
}

func TestFileViewTracksTheReturnedReadRatherThanASecondRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	views, overlay := NewFileViews(), &changingReadOverlay{}
	if err := runViewTool(t, readFile{views: views, overlay: overlay}, map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	err := runViewTool(t, writeFile{views: views, overlay: overlay}, map[string]string{"path": path, "content": "overwrite\n"})
	if !errors.Is(err, ErrFileChangedSinceSeen) || overlay.writes != 0 {
		t.Fatalf("stale read certified a later version: %v; writes=%d", err, overlay.writes)
	}
}

func TestStrictOverwriteOfPartlyReadFileNamesTheWholeRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "long.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", readFileDefaultLimit+500)), 0600); err != nil {
		t.Fatal(err)
	}
	views := NewStrictFileViews()
	reader, writer := readFile{workDir: root, views: views}, writeFile{workDir: root, views: views}
	if _, err := reader.Execute(t.Context(), json.RawMessage(`{"path":"long.txt"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := writer.Execute(t.Context(), json.RawMessage(`{"path":"long.txt","content":"short\n"}`))
	if !errors.Is(err, ErrFileNotFullySeen) || !errors.Is(err, ErrFileChangedSinceSeen) || !strings.Contains(err.Error(), "2500") {
		t.Fatalf("partial read refusal does not say how to certify the file: %v", err)
	}
	if _, err := reader.Execute(t.Context(), json.RawMessage(`{"path":"long.txt","limit":2500}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Execute(t.Context(), json.RawMessage(`{"path":"long.txt","content":"short\n"}`)); err != nil {
		t.Fatalf("whole read did not certify the overwrite: %v", err)
	}
}
