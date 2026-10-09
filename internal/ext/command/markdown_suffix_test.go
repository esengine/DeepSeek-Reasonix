package command

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestMarkdownExtensionCaseDoesNotEnterCommandName(t *testing.T) {
	for _, tc := range []struct{ path, name string }{
		{"review.md", "review"}, {"review.MD", "review"}, {"review.Md", "review"},
		{"git/Review.mD", "git:Review"}, {"release.MD/notes.v2.MD", "release.MD:notes.v2"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			root := testenv.TempDir(t)
			write(t, root, tc.path, "---\ndescription: Review fixture\n---\nREVIEW $ARGUMENTS")
			cmds, err := Load(root)
			if err != nil || len(cmds) != 1 {
				t.Fatalf("commands=%+v err=%v", cmds, err)
			}
			if c := cmds[0]; c.Name != tc.name || c.Source != filepath.Join(root, tc.path) || c.Render([]string{"issue"}) != "REVIEW issue" {
				t.Errorf("command=%+v, want name %q and unchanged source/body", c, tc.name)
			}
			inspection := Inspect(root)
			if len(inspection.Candidates) != 1 || inspection.Candidates[0].Name != tc.name || inspection.Candidates[0].Status != CandidateWinner {
				t.Errorf("inspection=%+v", inspection)
			}
			cmds, err = LoadRoots(Root{Path: root, Plugin: "case-kit"})
			if err != nil || len(cmds) != 2 {
				t.Fatalf("plugin commands=%+v err=%v", cmds, err)
			}
			for _, c := range cmds {
				want := "case-kit:" + tc.name
				if c.Hidden {
					want = tc.name
				}
				if c.Name != want || c.ShortName != tc.name || c.Plugin != "case-kit" {
					t.Errorf("plugin command=%+v, want name %q", c, want)
				}
			}
		})
	}
}

func TestUnreadableMarkdownCommandKeepsStemInDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions")
	}
	root := testenv.TempDir(t)
	write(t, root, "git/blocked.MD", "REVIEW $ARGUMENTS")
	path := filepath.Join(root, "git", "blocked.MD")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("process can read files regardless of mode")
	}
	inspection := Inspect(root)
	if len(inspection.Candidates) != 1 {
		t.Fatalf("inspection=%+v", inspection)
	}
	c := inspection.Candidates[0]
	if c.Name != "git:blocked" || c.Status != CandidateError || c.Path != path || c.Error == "" {
		t.Errorf("unreadable candidate=%+v", c)
	}
}
