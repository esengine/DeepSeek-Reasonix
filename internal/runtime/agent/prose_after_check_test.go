package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

func TestNamedWriteThroughLinkNamesItsTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "README.md", filepath.Join("src", "a.md")} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("neutral\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "notes.md")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		path    string
		success bool
		want    string
	}{
		{name: "linked file", path: "notes.md", success: true, want: filepath.Join(root, "main.go")},
		{name: "linked file, write reported failed", path: "notes.md", want: filepath.Join(root, "main.go")},
		{name: "linked directory", path: filepath.Join("docs", "a.md"), success: true, want: filepath.Join(root, "src", "a.md")},
		{name: "plain file", path: "README.md", success: true},
		{name: "missing file", path: "gone.md", success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := evidence.Receipt{ToolName: "write_file", Success: tc.success, Mutation: true, Write: true, MutationEvidence: evidence.MutationProven, Paths: []string{tc.path}}
			decorateObservedPaths(&rec, &toolCallPlan{pathsBefore: pathSnapshot{root: root}})
			added := slices.DeleteFunc(slices.Clone(rec.Paths), func(p string) bool { return p == tc.path })
			if tc.want == "" && len(added) != 0 {
				t.Errorf("paths = %v, want only %s", rec.Paths, tc.path)
			}
			if tc.want != "" && !slices.Contains(added, tc.want) {
				t.Errorf("paths = %v, want the link's target %s", rec.Paths, tc.want)
			}
		})
	}
}
