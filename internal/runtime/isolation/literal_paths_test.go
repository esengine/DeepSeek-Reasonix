package isolation

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestApplyToolWritesOnlyItsLiteralDeclaredFile(t *testing.T) {
	for _, name := range []string{"[a].txt", "*.txt", ":(top)a.txt", "line\nfile.txt"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.ContainsAny(name, "*:\n") {
				t.Skip("literal filename is not representable on Windows")
			}
			repo := gitRepo(t)
			s := NewStore(testenv.TempDir(t), repo, opened(t, repo))
			report, err := s.Execute(t.Context(), func(_ context.Context, run Run) (Outcome, error) {
				write(t, run.WorkspaceRoot, name, "literal result\n")
				return Outcome{}, nil
			}, repo, "create literal file", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			apply := NewApplyTool(s)
			paths, err := apply.DeclaredWritePaths(t.Context(), args(id))
			if err != nil || !slices.Equal(paths, []string{filepath.Join(s.repo.Top().WorkTree, name)}) {
				t.Fatalf("declared literal paths = %q, %v", paths, err)
			}
			write(t, repo, "a.txt", "user content outside declaration\n")
			if _, err := apply.Execute(t.Context(), args(id)); err != nil {
				t.Fatal(err)
			}
			if got := read(t, repo, name); got != "literal result\n" {
				t.Fatalf("apply tool missed the declared literal file: %q", got)
			}
			if got := read(t, repo, "a.txt"); got != "user content outside declaration\n" {
				t.Fatalf("apply tool wrote outside declared paths: %q", got)
			}
			if slices.Contains(s.Pending(), id) {
				t.Fatal("successfully applied literal result remained pending")
			}
			if _, err := os.Stat(filepath.Join(repo, name)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
