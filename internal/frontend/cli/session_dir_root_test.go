package cli

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// A session started in a repository's subdirectory belongs to the repository:
// boot resolves the workspace to the git root, and a session dir keyed to the
// raw working directory filed the transcript where that workspace never lists.
func TestCLISessionDirFollowsTheResolvedWorkspaceRoot(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	repo := testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "desktop", "frontend")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	want := config.ProjectSessionDir(repo)
	if got := resolveCLISessionDir(); got != want {
		t.Fatalf("session dir from a subdirectory = %s, want the repository's %s", got, want)
	}
	// --dir pins the root to the directory itself, and the session dir follows it.
	if got, want := resolveCLISessionDirFor(sub), config.ProjectSessionDir(sub); got != want {
		t.Fatalf("session dir for --dir %s = %s, want %s", sub, got, want)
	}
}
