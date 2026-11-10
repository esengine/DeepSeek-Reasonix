package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

// A folder that carries a .git git will not open keeps an empty identity, and
// says that git refused it rather than that there is no repository.
func TestWorkspaceRepoSaysWhyAMarkedFolderHasNoIdentity(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ./nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := workspaceRepo(context.Background(), gitcmd.Repo{}, dir)
	if repo.Valid() || repo.Absent != gitcmd.AbsenceRefused || repo.AbsentDetail == "" {
		t.Fatalf("repo = %+v, want an empty identity that says git refused it", repo)
	}
	plain := workspaceRepo(context.Background(), gitcmd.Repo{}, testenv.TempDir(t))
	if plain.Valid() || plain.Absent != "" {
		t.Fatalf("a folder with no marker = %+v, want an empty identity with no absence", plain)
	}
}
