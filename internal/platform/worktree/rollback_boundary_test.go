package worktree

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestDiscardUnpublishedRejectsWrongManagedBoundary(t *testing.T) {
	requireGit(t)
	for _, name := range []string{"outside", "root itself", "shared prefix"} {
		t.Run(name, func(t *testing.T) {
			repo := opened(t, initRepo(t))
			managed := filepath.Join(testenv.TempDir(t), "managed-extra")
			result, err := Create(t.Context(), repo, managed)
			if err != nil {
				t.Fatal(err)
			}
			wrong := testenv.TempDir(t)
			switch name {
			case "root itself":
				wrong = result.WorktreeRoot
			case "shared prefix":
				wrong = filepath.Join(filepath.Dir(managed), "managed")
			}
			if err := DiscardUnpublished(t.Context(), repo, wrong, result); err == nil {
				t.Fatal("cleanup accepted a result outside its managed boundary")
			}
			if _, err := os.Stat(filepath.Join(result.WorktreeRoot, "README.md")); err != nil {
				t.Fatalf("wrong-boundary cleanup touched the workspace: %v", err)
			}
			if err := DiscardUnpublished(t.Context(), repo, managed, result); err != nil {
				t.Fatalf("owning boundary could not clean its workspace: %v", err)
			}
		})
	}
}
