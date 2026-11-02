package isolation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestApplyToolRetainsResultAfterAncestorReplacement(t *testing.T) {
	repo := gitRepo(t)
	write(t, repo, "nested/victim.txt", "original\n")
	managed := testenv.TempDir(t)
	s := NewStore(managed, repo, opened(t, repo))
	report, err := s.Execute(t.Context(), func(_ context.Context, run Run) (Outcome, error) {
		write(t, run.WorkspaceRoot, "a.txt", "candidate\n")
		return Outcome{}, os.Remove(filepath.Join(run.WorkspaceRoot, "nested/victim.txt"))
	}, repo, "delete a nested file", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	e, err := s.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	outside := testenv.TempDir(t)
	write(t, outside, "victim.txt", "original\n")
	parent, saved := filepath.Join(repo, "nested"), filepath.Join(repo, "saved")
	if err := os.Rename(parent, saved); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", parent, outside).CombinedOutput()
		if err != nil {
			t.Fatalf("create directory junction: %v %s", err, out)
		}
	} else if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	resumed := NewStore(managed, repo, opened(t, repo))
	_, err = NewApplyTool(resumed).Execute(t.Context(), args(id))
	if got := read(t, outside, "victim.txt"); got != "original\n" {
		t.Fatalf("external file changed: %q", got)
	}
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeApplyConflict {
		t.Fatalf("apply did not report the ancestor conflict: %v", err)
	}
	if !slices.Contains(resumed.Pending(), id) {
		t.Fatal("result lost its original pending ID")
	}
	if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
		t.Fatalf("candidate lost after refusal: %v", err)
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Fatalf("refusal wrote sibling: %q", got)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApplyTool(NewStore(managed, repo, opened(t, repo))).Execute(t.Context(), args(id)); err != nil {
		t.Fatalf("same-ID retry: %v", err)
	}
	if read(t, repo, "a.txt") != "candidate\n" {
		t.Fatal("retry did not apply sibling")
	}
	if _, err := os.Stat(filepath.Join(repo, "nested/victim.txt")); !os.IsNotExist(err) {
		t.Fatalf("retry did not delete intended path: %v", err)
	}
	if got := read(t, outside, "victim.txt"); got != "original\n" {
		t.Fatalf("retry changed external file: %q", got)
	}
}
