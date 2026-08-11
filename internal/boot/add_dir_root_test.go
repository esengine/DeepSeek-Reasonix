package boot

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAdditionalDirsResolveAgainstExplicitWorkspaceRoot pins the --dir/--add-dir
// contract that #6431 threads through cliBuildOverrides.WorkspaceRoot: when --dir
// points into a git repo, a relative --add-dir must resolve under that explicit
// root, not the repo's git root.
func TestAdditionalDirsResolveAgainstExplicitWorkspaceRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The --dir target: a subdirectory of the git repo, with a sibling to add.
	proj := filepath.Join(repo, "packages", "app")
	rel := filepath.Join(proj, "shared")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}

	// An explicit workspace root (from --dir) pins resolution to proj, not the git root.
	root := resolveWorkspaceRoot(proj)
	if root != proj {
		t.Fatalf("resolveWorkspaceRoot(%q) = %q, want the explicit --dir", proj, root)
	}

	// A relative --add-dir resolves against that explicit root (proj/shared), not
	// repo/shared under the git root.
	got, err := normalizeAdditionalDirs(root, []string{"shared"})
	if err != nil {
		t.Fatalf("normalizeAdditionalDirs: %v", err)
	}
	want, err := filepath.EvalSymlinks(rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("additional dirs = %v, want [%s] under --dir (not the git root)", got, want)
	}
}
