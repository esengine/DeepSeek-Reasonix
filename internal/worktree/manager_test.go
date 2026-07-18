package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerRejectsDirtySourceForSubagentWorktree(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(t.TempDir())
	_, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if !errors.Is(err, ErrDirtySource) {
		t.Fatalf("Create err = %v, want ErrDirtySource", err)
	}
	list, listErr := mgr.List(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list) != 0 {
		t.Fatalf("dirty source should not create resources, got %d", len(list))
	}
}

func TestManagerCreatesInspectableSubagentResource(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	managed := t.TempDir()
	mgr := NewManager(managed)

	res, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindSubagent {
		t.Fatalf("kind = %q", res.Kind)
	}
	if res.IsolationID == "" {
		t.Fatal("isolation id is empty")
	}
	if !strings.HasPrefix(res.Branch, "reasonix/subagent-") {
		t.Fatalf("branch = %q", res.Branch)
	}
	if res.WorkspaceRoot != res.WorktreeRoot {
		t.Fatalf("workspace root = %q, worktree root = %q", res.WorkspaceRoot, res.WorktreeRoot)
	}
	if res.SourceRoot == res.WorktreeRoot || res.SourceRoot == "" {
		t.Fatalf("source root = %q, worktree root = %q", res.SourceRoot, res.WorktreeRoot)
	}
	if res.BaseCommit == "" || res.HeadCommit == "" || res.BaseCommit != res.HeadCommit {
		t.Fatalf("commits = base %q head %q", res.BaseCommit, res.HeadCommit)
	}
	if res.LifecycleState != LifecycleStateCreated || res.CleanupState != CleanupStateActive || res.CreatedAt.IsZero() {
		t.Fatalf("unexpected lifecycle: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.WorktreeRoot, "README.md")); err != nil {
		t.Fatalf("worktree missing committed file: %v", err)
	}

	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].IsolationID != res.IsolationID {
		t.Fatalf("List = %+v, want one resource %q", list, res.IsolationID)
	}
	shown, err := mgr.Show(ctx, res.IsolationID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Branch != res.Branch {
		t.Fatalf("Show branch = %q, want %q", shown.Branch, res.Branch)
	}
	status, err := mgr.Status(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	if status.Dirty {
		t.Fatalf("new worktree status dirty: %+v", status)
	}

	if err := os.WriteFile(filepath.Join(res.WorktreeRoot, "README.md"), []byte("child edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = mgr.Status(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Dirty || len(status.Paths) != 1 || status.Paths[0] != "README.md" {
		t.Fatalf("dirty status = %+v", status)
	}
	diff, err := mgr.Diff(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+child edit") || len(diff.Paths) != 1 || diff.Paths[0] != "README.md" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestManagerApplyCleanPatchAndReportConflictWithoutPollution(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(t.TempDir())

	first, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.WorktreeRoot, "README.md"), []byte("first writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := mgr.Apply(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ApplyStatusApplied || len(result.Paths) != 1 || result.Paths[0] != "README.md" {
		t.Fatalf("first apply result = %+v", result)
	}
	sourceBytes, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(sourceBytes), "\r\n", "\n") != "first writer\n" {
		t.Fatalf("source content after clean apply = %q", sourceBytes)
	}

	if err := os.WriteFile(filepath.Join(second.WorktreeRoot, "README.md"), []byte("second writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = mgr.Apply(ctx, second)
	if !errors.Is(err, ErrApplyConflict) {
		t.Fatalf("second apply err = %v, want ErrApplyConflict", err)
	}
	if result.Status != ApplyStatusConflicted || len(result.ConflictedPaths) != 1 || result.ConflictedPaths[0] != "README.md" {
		t.Fatalf("second apply result = %+v", result)
	}
	sourceBytes, err = os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(sourceBytes), "\r\n", "\n") != "first writer\n" {
		t.Fatalf("conflicting apply polluted source content = %q", sourceBytes)
	}
	status := gitStatus(t, repo)
	if strings.Contains(status, "UU ") || strings.Contains(status, "<<<<<<<") {
		t.Fatalf("source repo has unresolved conflict state: %q", status)
	}
}

func TestLegacyCreateUsesDeliveryPolicyAndAllowsDirtyCommittedHead(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Create(ctx, repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Branch, "reasonix/delivery-") {
		t.Fatalf("branch = %q", result.Branch)
	}
	if !result.SourceDirty {
		t.Fatalf("SourceDirty = false, want true")
	}
	if _, err := os.Stat(filepath.Join(result.WorktreeRoot, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatalf("dirty source file must not be copied, stat err = %v", err)
	}
}

func gitStatus(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "status", "--porcelain=v1", "--untracked-files=normal")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}
