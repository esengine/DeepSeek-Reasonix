package isolation

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestRecoveryInspectionFailureRetainsResultAndCanRetry(t *testing.T) {
	for _, operation := range []string{"paths", "apply", "discard"} {
		t.Run(operation, func(t *testing.T) {
			repo := gitRepo(t)
			managed := testenv.TempDir(t)
			store := NewStore(managed, repo, opened(t, repo))
			report, err := store.Execute(t.Context(), writes(map[string]string{"a.txt": "isolated work\n"}), repo, "change a.txt", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			e, err := store.lookup(id)
			if err != nil {
				t.Fatal(err)
			}
			marker := entryPath(e.Cand)
			before, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			restarted := NewStore(managed, repo, opened(t, repo))
			invoke := func() error {
				switch operation {
				case "paths":
					_, err := NewApplyTool(restarted).DeclaredWritePaths(t.Context(), args(id))
					return err
				case "apply":
					_, err := NewApplyTool(restarted).Execute(t.Context(), args(id))
					return err
				default:
					_, err := NewDiscardTool(restarted).Execute(t.Context(), args(id))
					return err
				}
			}
			// No Git process can inspect or clean the worktree while PATH is empty.
			// This is a temporary tool failure, not an empty or discarded result.
			previousPath := os.Getenv("PATH")
			t.Setenv("PATH", testenv.TempDir(t))
			err = invoke()
			after, readErr := os.ReadFile(marker)
			if readErr != nil || !bytes.Equal(after, before) {
				t.Fatalf("inspection failure lost or changed the recovery marker: %v", readErr)
			}
			if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "isolated work\n" {
				t.Fatalf("candidate work was changed: %q", got)
			}
			if got := read(t, repo, "a.txt"); got != "one\n" {
				t.Fatalf("failed recovery changed workspace: %q", got)
			}
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != CodeUnavailable || !strings.Contains(refusal.Message, id) || !strings.Contains(refusal.Message, "retained") {
				t.Fatalf("inspection error = %v, want unavailable result with retained-files guidance", err)
			}
			if !slices.Contains(restarted.Pending(), id) {
				t.Fatal("pending result became undiscoverable")
			}
			t.Setenv("PATH", previousPath)
			if err := invoke(); err != nil {
				t.Fatalf("same result could not retry after Git returned: %v", err)
			}
			if operation == "apply" && read(t, repo, "a.txt") != "isolated work\n" {
				t.Fatal("retried apply did not integrate retained work")
			}
			if operation == "paths" {
				if _, err := NewDiscardTool(restarted).Execute(t.Context(), args(id)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), entryFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settled result marker remains: %v", err)
			}
		})
	}
}
