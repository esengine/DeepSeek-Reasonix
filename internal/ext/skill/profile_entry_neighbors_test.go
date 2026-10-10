package skill

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestReadEntryProfileNeighbours(t *testing.T) {
	root := testenv.TempDir(t)
	for _, item := range []struct{ path, body string }{
		{"valid/SKILL.md", "BODY"},
		{"marked.md", "---\ndescription: A marker\n---\nBODY"},
		{"notes.md", "Plain documentation"},
		{"bad name.md", "---\nname: valid\n---\nBODY"},
		{"bad name/SKILL.md", "---\nname: valid\n---\nBODY"},
		{"container/notes.txt", "BODY"},
	} {
		writeSkill(t, root, item.path, item.body)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("missing", filepath.Join(root, "broken.md")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	st := New(Options{HomeDir: testenv.TempDir(t), DisableBuiltins: true, Stderr: io.Discard})
	for _, marker := range []bool{false, true} {
		for _, entry := range entries {
			got, ok := st.readEntry(root, ScopeCustom, marker, entry)
			want := entry.Name() == "valid" || entry.Name() == "marked.md" || (!marker && entry.Name() == "notes.md")
			if ok != want {
				t.Errorf("%s marker=%v loaded=%v want=%v", entry.Name(), marker, ok, want)
			}
			if ok && got.Path != filepath.Join(root, entry.Name()) && got.Path != filepath.Join(root, entry.Name(), SkillFile) {
				t.Errorf("unexpected path=%s", got.Path)
			}
		}
	}
}
