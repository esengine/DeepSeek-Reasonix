package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
)

// opened is dir's identity as a session opening it resolves it.
func opened(t *testing.T, dir string) gitcmd.Repo {
	t.Helper()
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		return gitcmd.Repo{Dir: dir}
	}
	return repo
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	write(t, repo, "a.txt", "one\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "init"}} {
		cmd := gitcmd.Command(context.Background(), repo, args...)
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// writes returns a runner that writes each file into the worktree it is given.
func writes(files map[string]string) Runner {
	return func(_ context.Context, run Run) (Outcome, error) {
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(run.WorkspaceRoot, name), []byte(body), 0o644); err != nil {
				return Outcome{}, err
			}
		}
		return Outcome{Answer: "did it"}, nil
	}
}

var isoID = regexp.MustCompile(`iso_[0-9a-f]+`)

func idOf(t *testing.T, report string) string {
	t.Helper()
	id := isoID.FindString(report)
	if id == "" {
		t.Fatalf("report names no id:\n%s", report)
	}
	return id
}

func args(id string) json.RawMessage { return json.RawMessage(`{"id":"` + id + `"}`) }

// The run's changes stay out of the workspace until they are applied, and the
// apply writes them beside whatever the workspace did meanwhile.
func TestIsolatedRunIsHeldUntilApplied(t *testing.T) {
	repo := gitRepo(t)
	store := NewStore(testenv.TempDir(t), repo, opened(t, repo))
	ctx := context.Background()

	report, err := store.Execute(ctx, writes(map[string]string{"a.txt": "isolated\n", "b.txt": "new\n"}), repo, "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	if !strings.Contains(report, "M a.txt +1 -1") || !strings.Contains(report, "A b.txt +1 -0") || !strings.Contains(report, "did it") {
		t.Fatalf("report does not describe the run:\n%s", report)
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Fatalf("the run reached the workspace before apply: a.txt = %q", got)
	}
	write(t, repo, "parent.txt", "the parent kept working\n")

	apply := NewApplyTool(store)
	paths, err := apply.DeclaredWritePaths(ctx, args(id))
	if err != nil || len(paths) != 2 {
		t.Fatalf("declared paths = %v, %v; want both changed files", paths, err)
	}
	if _, err := apply.Execute(ctx, args(id)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if read(t, repo, "a.txt") != "isolated\n" || read(t, repo, "b.txt") != "new\n" {
		t.Fatal("apply did not write the run's changes")
	}
	if len(store.Pending()) != 0 {
		t.Fatalf("an applied result is still pending: %v", store.Pending())
	}
}

// A path the parent changed too refuses the apply with a code, writes nothing,
// and keeps the result so it can still be discarded.
func TestApplyConflictKeepsTheResult(t *testing.T) {
	repo := gitRepo(t)
	store := NewStore(testenv.TempDir(t), repo, opened(t, repo))
	ctx := context.Background()

	report, err := store.Execute(ctx, writes(map[string]string{"a.txt": "isolated\n"}), repo, "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	write(t, repo, "a.txt", "parent\n")

	_, err = NewApplyTool(store).Execute(ctx, args(id))
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeApplyConflict || !strings.Contains(refusal.Message, "a.txt") {
		t.Fatalf("apply err = %v, want %s naming a.txt", err, CodeApplyConflict)
	}
	if read(t, repo, "a.txt") != "parent\n" {
		t.Fatal("a refused apply wrote a.txt")
	}
	if _, err := NewDiscardTool(store).Execute(ctx, args(id)); err != nil {
		t.Fatalf("discard after conflict: %v", err)
	}
	if _, err := NewDiscardTool(store).Execute(ctx, args(id)); !errors.As(err, &refusal) || refusal.Code != CodeUnknownID {
		t.Fatalf("second discard err = %v, want %s", err, CodeUnknownID)
	}
}

// A run that changed nothing leaves nothing to settle.
func TestUnchangedRunLeavesNoEntry(t *testing.T) {
	repo := gitRepo(t)
	store := NewStore(testenv.TempDir(t), repo, opened(t, repo))
	report, err := store.Execute(context.Background(), writes(nil), repo, "look", "")
	if err != nil {
		t.Fatal(err)
	}
	if isoID.MatchString(report) || len(store.Pending()) != 0 {
		t.Fatalf("an unchanged run left an entry: %q, %v", report, store.Pending())
	}
}

// A pending result is its worktree, not the store's memory of it: a store
// built afresh, as after a restart, finds it by id and can apply it.
func TestPendingResultOutlivesItsStore(t *testing.T) {
	repo := gitRepo(t)
	managed := testenv.TempDir(t)
	report, err := NewStore(managed, repo, opened(t, repo)).Execute(context.Background(), writes(map[string]string{"a.txt": "isolated\n"}), repo, "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	resumed := NewStore(managed, repo, opened(t, repo))
	if got := resumed.Pending(); len(got) != 1 || got[0] != id {
		t.Fatalf("pending after rebuild = %v, want [%s]", got, id)
	}
	if _, err := NewApplyTool(resumed).Execute(context.Background(), args(id)); err != nil {
		t.Fatalf("apply after rebuild: %v", err)
	}
	if read(t, repo, "a.txt") != "isolated\n" {
		t.Fatal("the rebuilt store did not apply the result")
	}
	if left, _ := filepath.Glob(filepath.Join(managed, "*", "*")); len(left) != 0 {
		t.Fatalf("an applied result left %v behind", left)
	}
}

// A result taken from another workspace is never offered: applying it would
// write into that workspace from this one.
func TestPendingResultsBelongToTheirWorkspace(t *testing.T) {
	repo, other := gitRepo(t), gitRepo(t)
	managed := testenv.TempDir(t)
	report, err := NewStore(managed, repo, opened(t, repo)).Execute(context.Background(), writes(map[string]string{"a.txt": "x\n"}), repo, "do it", "")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := NewStore(managed, other, opened(t, other))
	if got := elsewhere.Pending(); len(got) != 0 {
		t.Fatalf("another workspace sees %v", got)
	}
	var refusal tool.Refusal
	if _, err := NewApplyTool(elsewhere).Execute(context.Background(), args(idOf(t, report))); !errors.As(err, &refusal) || refusal.Code != CodeUnknownID {
		t.Fatalf("apply from another workspace: %v", err)
	}
}

// Isolation is never faked: a workspace git cannot snapshot is refused, and
// the runner is never started.
func TestNonGitWorkspaceIsRefused(t *testing.T) {
	store := NewStore(testenv.TempDir(t), testenv.TempDir(t), gitcmd.Repo{})
	ran := false
	_, err := store.Execute(context.Background(), func(context.Context, Run) (Outcome, error) {
		ran = true
		return Outcome{}, nil
	}, testenv.TempDir(t), "do it", "")
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeNotGit || ran {
		t.Fatalf("err = %v, ran = %v; want %s and no run", err, ran, CodeNotGit)
	}
}
