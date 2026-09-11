package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func gitScopeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "init"},
		{"branch", "shared"},
	} {
		if out, err := workspaceGit(append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	return root
}

func TestWorkspaceGitBranchScope(t *testing.T) {
	rootA, rootB := gitScopeRepo(t), gitScopeRepo(t)
	a := &App{tabs: map[string]*WorkspaceTab{
		"a": {WorkspaceRoot: rootA}, "b": {WorkspaceRoot: rootB},
	}}
	// The requested tab owns the target even when it is not active.
	list, err := a.GitBranchesForTab("a", rootA)
	if err != nil || !slices.Contains(list, "shared") {
		t.Fatalf("branches: %v, %v", list, err)
	}
	base, err := a.gitWorkspaceBaseForTab("a", rootA)
	if err != nil {
		t.Fatal(err)
	}
	a.tabs["a"].WorkspaceRoot = rootB
	if err := a.GitCheckoutForTab("a", rootA, "shared"); err == nil {
		t.Fatal("accepted stale workspace identity")
	}
	if err := workspaceCheckoutBranch(base, "shared", false); err != nil {
		t.Fatal(err)
	}
	if workspaceGitBranch(rootA) != "shared" || workspaceGitBranch(rootB) != "main" {
		t.Fatal("checkout changed the replacement workspace")
	}
	if err := a.GitCreateBranchForTab("b", rootB, "new-branch"); err != nil {
		t.Fatal(err)
	}
	if workspaceGitBranch(rootA) != "shared" || workspaceGitBranch(rootB) != "new-branch" {
		t.Fatal("create crossed project boundaries")
	}
	if err := a.GitCheckoutForTab("a", rootB, "--detach"); err == nil {
		t.Fatal("accepted option as branch")
	}
	for _, id := range []string{"", "missing", "relative", "deleted"} {
		a.tabs["relative"] = &WorkspaceTab{WorkspaceRoot: "."}
		a.tabs["deleted"] = &WorkspaceTab{WorkspaceRoot: filepath.Join(t.TempDir(), "gone")}
		if _, err := a.GitBranchesForTab(id, rootA); err == nil {
			t.Fatalf("accepted %q", id)
		}
		if err := a.GitCheckoutForTab(id, rootA, "shared"); err == nil {
			t.Fatalf("checkout accepted %q", id)
		}
		if err := a.GitCreateBranchForTab(id, rootA, "new"); err == nil {
			t.Fatalf("create accepted %q", id)
		}
	}
}

func TestWorkspaceTallyBudgetAndFileKinds(t *testing.T) {
	base := gitScopeRepo(t)
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, fixture := range []struct {
		name, body string
		want       int
		partial    bool
	}{
		{"empty", "", 0, false}, {"text", "one\ntwo", 2, false},
		{"binary", "one\n\x00two", 0, false},
		{"large", strings.Repeat("x\n", workspaceDiffTallyFileLimit/2) + "tail", workspaceDiffTallyFileLimit / 2, true},
	} {
		if err := root.WriteFile(fixture.name, []byte(fixture.body), 0600); err != nil {
			t.Fatal(err)
		}
		remaining := int64(workspaceDiffTallyReadLimit)
		count, partial := workspaceCountFileLines(context.Background(), root, fixture.name, &remaining)
		if count != fixture.want || partial != fixture.partial || remaining < workspaceDiffTallyReadLimit-workspaceDiffTallyFileLimit {
			t.Fatalf("%s: count=%d partial=%v remaining=%d", fixture.name, count, partial, remaining)
		}
	}
	for _, path := range []string{".git", "../outside", "missing"} {
		remaining := int64(100)
		if n, partial := workspaceCountFileLines(context.Background(), root, path, &remaining); n != 0 || !partial || remaining != 100 {
			t.Fatalf("read nonregular path %s", path)
		}
	}
	if err := root.Symlink("text", "link"); err == nil {
		remaining := int64(100)
		if n, partial := workspaceCountFileLines(context.Background(), root, "link", &remaining); n != 0 || !partial || remaining != 100 {
			t.Fatal("followed symlink")
		}
		if f, err := openWorkspaceTallyFile(root, "link"); err == nil {
			f.Close()
			t.Fatal("platform open followed symlink")
		}
	}
	paths := make([]string, 17)
	for i := range paths {
		paths[i] = "large"
	}
	added, _, partial := workspaceGitDiffTally(context.Background(), base, paths)
	if added != workspaceDiffTallyReadLimit/2 || !partial {
		t.Fatalf("aggregate budget: %d, %v", added, partial)
	}
}

func TestWorkspaceChangesExpiredDeadlineIsIncomplete(t *testing.T) {
	base := gitScopeRepo(t)
	a := &App{tabs: map[string]*WorkspaceTab{"a": {WorkspaceRoot: base}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := a.workspaceChanges(ctx, "a")
	if view.GitAvailable || !view.Incomplete || view.GitErr == "" {
		t.Fatalf("expired scan: %+v", view)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	remaining := int64(100)
	if _, partial := workspaceCountFileLines(ctx, root, "anything", &remaining); !partial || remaining != 100 {
		t.Fatal("read after deadline")
	}
}
