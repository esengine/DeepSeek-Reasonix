package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// The auto workspace is where an unchosen launch opens, not a project anyone
// picked: if the list remembered it, it would become the answer to every later
// launch and sit in the sidebar as a project.
func TestTheAutoWorkspaceIsNeverRemembered(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	auto := config.AutoWorkspaceDir()
	if err := os.MkdirAll(auto, 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(testenv.TempDir(t), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := addRememberedWorkspace(context.Background(), auto); err != nil {
		t.Fatal(err)
	}
	keepUsedWorkspace(auto)
	if got := LaunchWorkspaces(); len(got) != 0 {
		t.Fatalf("the auto workspace was remembered: %v", got)
	}

	if err := addRememberedWorkspace(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if got := LaunchWorkspaces(); len(got) != 1 || got[0] != project {
		t.Fatalf("a chosen project must still be remembered: %v", got)
	}
}

// What is said in an auto-workspace conversation is kept in the user state
// root, with the conversations of every other folder: nothing about it lives in
// a directory that can be deleted out from under it.
func TestAutoWorkspaceConversationsAreKeptUnderTheStateRoot(t *testing.T) {
	state := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", state)
	dir := SessionDirFor(config.AutoWorkspaceDir())
	if rel, err := filepath.Rel(state, dir); err != nil || len(rel) == 0 || rel[0] == '.' {
		t.Fatalf("session dir %q is not under the state root %q", dir, state)
	}
}
