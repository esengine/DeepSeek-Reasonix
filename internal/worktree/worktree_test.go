package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Reasonix Test", "GIT_AUTHOR_EMAIL=reasonix@example.invalid",
			"GIT_COMMITTER_NAME=Reasonix Test", "GIT_COMMITTER_EMAIL=reasonix@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "initial")
	return repo
}

func TestCreateManagedWorktreeFromRepositoryFolder(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	result, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	wantSource, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceRoot != wantSource {
		t.Fatalf("source root = %q, want %q", result.SourceRoot, wantSource)
	}
	if result.WorkspaceRoot != result.WorktreeRoot {
		t.Fatalf("workspace root = %q, worktree root = %q", result.WorkspaceRoot, result.WorktreeRoot)
	}
	if !strings.HasPrefix(result.Branch, "reasonix/delivery-") {
		t.Fatalf("branch = %q", result.Branch)
	}
	if _, err := os.Stat(filepath.Join(result.WorktreeRoot, "README.md")); err != nil {
		t.Fatalf("created worktree missing committed file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.WorktreeRoot, ".reasonix-source")); !os.IsNotExist(err) {
		t.Fatalf("worktree must not contain a Reasonix marker, stat err = %v", err)
	}
	branchCmd := exec.Command("git", "-C", result.WorktreeRoot, "branch", "--show-current")
	branchOut, err := branchCmd.Output()
	if err != nil || strings.TrimSpace(string(branchOut)) != result.Branch {
		t.Fatalf("worktree branch = %q err=%v, want %q", strings.TrimSpace(string(branchOut)), err, result.Branch)
	}
}

func TestCreatePreservesSelectedRepositorySubdirectory(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	subdir := filepath.Join(repo, "packages", "app")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "app.txt"), []byte("app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	cmd = exec.Command("git", "-C", repo, "-c", "user.name=Reasonix Test", "-c", "user.email=reasonix@example.invalid", "commit", "-m", "subdir")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
	result, err := Create(context.Background(), subdir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(result.WorktreeRoot, "packages", "app")
	if result.WorkspaceRoot != want {
		t.Fatalf("workspace root = %q, want %q", result.WorkspaceRoot, want)
	}
}

func TestInspectRejectsUncommittedSelectedSubdirectoryWithoutGitMutation(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	untracked := filepath.Join(repo, "untracked-project")
	if err := os.MkdirAll(untracked, 0o755); err != nil {
		t.Fatal(err)
	}
	before, _, err := runGit(context.Background(), repo, "for-each-ref", "--format=%(refname)", "refs/heads/reasonix/delivery-")
	if err != nil {
		t.Fatal(err)
	}
	got := Inspect(context.Background(), untracked)
	if got.Available || !strings.Contains(got.Reason, "committed HEAD") {
		t.Fatalf("availability = %+v", got)
	}
	after, _, err := runGit(context.Background(), repo, "for-each-ref", "--format=%(refname)", "refs/heads/reasonix/delivery-")
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("availability probe mutated Git refs: before=%q after=%q", before, after)
	}
}

func TestCreateFromExistingLinkedWorktree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	linked := filepath.Join(t.TempDir(), "already-linked-worktree")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-b", "test/existing-linked", linked, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v %s", err, out)
	}

	result, err := Create(context.Background(), linked, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantSource, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceRoot != wantSource {
		t.Fatalf("source root = %q, want linked worktree %q", result.SourceRoot, wantSource)
	}
	if result.WorkspaceRoot != result.WorktreeRoot {
		t.Fatalf("workspace root = %q, worktree root = %q", result.WorkspaceRoot, result.WorktreeRoot)
	}
	if result.WorktreeRoot == wantSource {
		t.Fatal("managed worktree reused the already-open linked worktree")
	}
	cmd = exec.Command("git", "-C", linked, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "test/existing-linked" {
		t.Fatalf("source linked-worktree branch changed: %q err=%v", strings.TrimSpace(string(out)), err)
	}
}

func TestCreateDoesNotCopyOrChangeDirtySource(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	dirtyPath := filepath.Join(repo, "README.md")
	if err := os.WriteFile(dirtyPath, []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Create(context.Background(), repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !result.SourceDirty {
		t.Fatal("dirty source was not reported")
	}
	got, err := os.ReadFile(filepath.Join(result.WorktreeRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Git for Windows may materialize the committed LF as CRLF according to
	// core.autocrlf. Compare the file's semantic content so this assertion only
	// detects the behavior under test: copying the dirty source value.
	if strings.TrimSpace(string(got)) != "base" {
		t.Fatalf("worktree copied uncommitted content: %q", got)
	}
	source, _ := os.ReadFile(dirtyPath)
	if string(source) != "uncommitted\n" {
		t.Fatalf("source was modified: %q", source)
	}
}

func TestInspectRejectsNonRepositoryAndUnbornRepository(t *testing.T) {
	requireGit(t)
	if got := Inspect(context.Background(), t.TempDir()); got.Available || !strings.Contains(got.Reason, "not inside a Git repository") {
		t.Fatalf("non-repo availability = %+v", got)
	}
	unborn := t.TempDir()
	if out, err := exec.Command("git", "-C", unborn, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if got := Inspect(context.Background(), unborn); got.Available || !strings.Contains(got.Reason, "initial commit") {
		t.Fatalf("unborn availability = %+v", got)
	}
}

func TestInspectWithoutGitExplainsSafeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PATH", t.TempDir())
	} else {
		t.Setenv("PATH", t.TempDir())
	}
	got := Inspect(context.Background(), t.TempDir())
	if got.Available || !strings.Contains(got.Reason, "Git is not installed") || !strings.Contains(got.Reason, "serialize writes") {
		t.Fatalf("no-Git availability = %+v", got)
	}
}

func TestManagedPathBoundary(t *testing.T) {
	managed := filepath.Join(t.TempDir(), "worktrees")
	inside := filepath.Join(managed, "repo", "id")
	sibling := managed + "-backup"
	if !IsManagedPath(inside, managed) {
		t.Fatal("inside path not recognized")
	}
	if IsManagedPath(managed, managed) || IsManagedPath(sibling, managed) {
		t.Fatal("managed root or prefix sibling was incorrectly recognized")
	}
}

func TestSafePathComponentHandlesWindowsNames(t *testing.T) {
	for input, want := range map[string]string{
		"repo:demo": "repo-demo",
		"CON":       "_CON",
		"lpt1.txt":  "_lpt1.txt",
		"a<b>c":     "a-b-c",
	} {
		if got := safePathComponent(input); got != want {
			t.Errorf("safePathComponent(%q) = %q, want %q", input, got, want)
		}
	}
}

// Argument construction — including the Windows-only core.longpaths override —
// now lives in internal/gitcmd and is covered by that package's tests.

func TestGitWorktreeAddUsesExtendedTimeout(t *testing.T) {
	if got := gitTimeout([]string{"status", "--porcelain=v1"}); got != gitProbeTimeout {
		t.Fatalf("status timeout = %v, want %v", got, gitProbeTimeout)
	}
	got := gitTimeout([]string{"worktree", "add", "-b", "branch", "destination", "HEAD"})
	if got != gitWorktreeMutationTimeout {
		t.Fatalf("worktree add timeout = %v, want %v", got, gitWorktreeMutationTimeout)
	}
	if remove := gitTimeout([]string{"worktree", "remove", "destination"}); remove != gitWorktreeMutationTimeout {
		t.Fatalf("worktree remove timeout = %v, want %v", remove, gitWorktreeMutationTimeout)
	}
	if got < 2*time.Minute || got <= gitProbeTimeout {
		t.Fatalf("worktree add timeout = %v, want a checkout-safe timeout longer than probes", got)
	}
}

func TestRollbackCreateRemovesOnlyUntouchedWorktree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	result, err := Create(context.Background(), repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := RollbackCreate(context.Background(), result); err != nil {
		t.Fatalf("RollbackCreate: %v", err)
	}
	if _, err := os.Stat(result.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after rollback: %v", err)
	}
	cmd := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+result.Branch)
	if err := cmd.Run(); err == nil {
		t.Fatalf("branch %q still exists after rollback", result.Branch)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("source repository was damaged: %v", err)
	}
}

func TestRollbackCreatePreservesChangedWorktree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	result, err := Create(context.Background(), repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	change := filepath.Join(result.WorktreeRoot, "uncommitted.txt")
	if err := os.WriteFile(change, []byte("preserve me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RollbackCreate(context.Background(), result); err == nil || !strings.Contains(err.Error(), "contains changes") {
		t.Fatalf("RollbackCreate error = %v, want changed-worktree refusal", err)
	}
	if got, err := os.ReadFile(change); err != nil || string(got) != "preserve me\n" {
		t.Fatalf("changed worktree was not preserved: data=%q err=%v", got, err)
	}
	cmd := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+result.Branch)
	if err := cmd.Run(); err != nil {
		t.Fatalf("changed worktree branch was removed: %v", err)
	}
}

func TestRollbackCreatePreservesIgnoredContent(t *testing.T) {
	requireGit(t)
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "file", path: "ignored.txt"},
		{name: "directory", path: filepath.Join("cache", "artifact.bin")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\ncache/\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git := exec.Command("git", "-C", repo, "add", ".gitignore")
			if out, err := git.CombinedOutput(); err != nil {
				t.Fatalf("git add .gitignore: %v %s", err, out)
			}
			git = exec.Command("git", "-C", repo, "-c", "user.name=Reasonix Test", "-c", "user.email=reasonix@example.invalid", "commit", "-m", "ignore generated files")
			if out, err := git.CombinedOutput(); err != nil {
				t.Fatalf("git commit .gitignore: %v %s", err, out)
			}

			result, err := Create(context.Background(), repo, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ignoredPath := filepath.Join(result.WorktreeRoot, tc.path)
			if err := os.MkdirAll(filepath.Dir(ignoredPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ignoredPath, []byte("preserve me\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := RollbackCreate(context.Background(), result); err == nil || !strings.Contains(err.Error(), "contains changes") {
				t.Fatalf("RollbackCreate error = %v, want ignored-content refusal", err)
			}
			if got, err := os.ReadFile(ignoredPath); err != nil || string(got) != "preserve me\n" {
				t.Fatalf("ignored content was not preserved: data=%q err=%v", got, err)
			}
			git = exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+result.Branch)
			if err := git.Run(); err != nil {
				t.Fatalf("ignored-content branch was removed: %v", err)
			}
		})
	}
}

func TestCreateSupportsPathsWithSpaces(t *testing.T) {
	requireGit(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo with spaces")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Reasonix Test", "GIT_AUTHOR_EMAIL=reasonix@example.invalid", "GIT_COMMITTER_NAME=Reasonix Test", "GIT_COMMITTER_EMAIL=reasonix@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	result, err := Create(context.Background(), repo, filepath.Join(parent, "managed worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && !strings.Contains(result.WorktreeRoot, "managed worktrees") {
		t.Fatalf("unexpected worktree root %q", result.WorktreeRoot)
	}
}
