package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProseMutationPathScope(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, tc := range []struct {
		name string
		root string
		path string
		want bool
	}{
		{name: "relative prose", root: root, path: "notes.md", want: true},
		{name: "deleted nested prose", root: root, path: "gone/note.rst", want: true},
		{name: "absolute prose", root: root, path: filepath.Join(root, "notes.md"), want: true},
		{name: "unknown root", path: "notes.md"},
		{name: "outside prose", root: root, path: filepath.Join(outside, "REASONIX.md")},
		{name: "relative escape", root: root, path: "../outside.md"},
		{name: "dotdot cannot conceal links", root: root, path: "alias/../notes.md"},
		{name: "VCS hooks", root: root, path: ".git/hooks/run.md"},
		{name: "nested VCS", root: root, path: "project/.hg/hooks/run.rst"},
		{name: "root inside VCS", root: filepath.Join(root, ".git"), path: "hooks/run.md"},
		{name: "code suffix", root: root, path: "gone.go"},
		{name: "uppercase suffix", root: root, path: "gone.MD", want: true},
		{name: "text input", root: root, path: "requirements.txt"},
		{name: "MDX component", root: root, path: "page.mdx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := proseMutationPath(tc.root, tc.path); got != tc.want {
				t.Errorf("scope = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProseMutationPathRejectsLinkedAncestor(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if proseMutationPath(root, "alias/deleted.md") {
		t.Fatal("linked ancestor waived verification")
	}
}
