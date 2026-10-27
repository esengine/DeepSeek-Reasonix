package worktree

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestWorktreePreparationOwnsItsIndex(t *testing.T) {
	for _, kind := range []string{"delivery", "candidate"} {
		for _, mode := range []string{"ambient_existing", "ambient_missing", "control"} {
			t.Run(kind+"_"+mode, func(t *testing.T) {
				requireGit(t)
				root := initRepo(t)
				repo := opened(t, root)
				snap, err := TakeSnapshot(t.Context(), repo)
				if err != nil {
					t.Fatal(err)
				}
				index := filepath.Join(repo.GitDir, "index")
				before, err := os.ReadFile(index)
				if err != nil {
					t.Fatal(err)
				}
				ambient := filepath.Join(testenv.TempDir(t), "ambient-index")
				var original []byte
				if mode == "ambient_existing" {
					writeRepoFile(t, root, "ambient-only.txt", "ambient staged content\n")
					for _, args := range [][]string{{"read-tree", "HEAD"}, {"add", "ambient-only.txt"}} {
						cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
						cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+ambient)
						if out, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("ambient fixture: %v %s", err, out)
						}
					}
					if err := os.Remove(filepath.Join(root, "ambient-only.txt")); err != nil {
						t.Fatal(err)
					}
					original, err = os.ReadFile(ambient)
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode != "control" {
					t.Setenv("GIT_INDEX_FILE", ambient)
				}
				managed := testenv.TempDir(t)
				var path string
				if kind == "delivery" {
					result, err := Create(t.Context(), repo, managed)
					if err != nil {
						t.Fatal(err)
					}
					path = result.WorktreeRoot
				} else {
					candidate, err := CreateCandidate(t.Context(), snap, managed)
					if err != nil {
						t.Fatal(err)
					}
					path = candidate.WorktreeRoot
				}
				if mode == "ambient_existing" {
					after, err := os.ReadFile(ambient)
					if err != nil || !bytes.Equal(original, after) {
						t.Fatal("preparing another worktree rewrote the ambient index")
					}
				}
				if mode == "ambient_missing" {
					if _, err := os.Stat(ambient); !os.IsNotExist(err) {
						t.Fatalf("preparation created an ambient index: %v", err)
					}
				}
				after, err := os.ReadFile(index)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("preparation changed the source index")
				}
				child := opened(t, path)
				if !child.Valid() {
					t.Fatal("prepared checkout has no own repository identity")
				}
				if _, err := os.Stat(filepath.Join(child.GitDir, "index")); err != nil {
					t.Fatalf("prepared checkout has no own index: %v", err)
				}
				if got := readRepoFile(t, path, "README.md"); got != "base\n" {
					t.Fatalf("checkout bytes=%q", got)
				}
			})
		}
	}
}
