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

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

// opened is dir's identity as a session opening it resolves it; a directory
// outside any work tree yields the unresolved Repo a session would hold.
func opened(t *testing.T, dir string) gitcmd.Repo {
	t.Helper()
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		return gitcmd.Repo{Dir: dir}
	}
	return repo
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := testenv.TempDir(t)
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
	managed := testenv.TempDir(t)
	result, err := Create(context.Background(), opened(t, repo), managed)
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
	result, err := Create(context.Background(), opened(t, subdir), testenv.TempDir(t))
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
	before, _, err := runGit(context.Background(), opened(t, repo), "for-each-ref", "--format=%(refname)", "refs/heads/reasonix/delivery-")
	if err != nil {
		t.Fatal(err)
	}
	got := Inspect(context.Background(), opened(t, untracked))
	if got.Available || !strings.Contains(got.Reason, "committed HEAD") {
		t.Fatalf("availability = %+v", got)
	}
	after, _, err := runGit(context.Background(), opened(t, repo), "for-each-ref", "--format=%(refname)", "refs/heads/reasonix/delivery-")
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
	linked := filepath.Join(testenv.TempDir(t), "already-linked-worktree")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-b", "test/existing-linked", linked, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v %s", err, out)
	}

	result, err := Create(context.Background(), opened(t, linked), testenv.TempDir(t))
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
	result, err := Create(context.Background(), opened(t, repo), testenv.TempDir(t))
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
	if got := Inspect(context.Background(), opened(t, testenv.TempDir(t))); got.Available || !strings.Contains(got.Reason, "not inside a Git repository") {
		t.Fatalf("non-repo availability = %+v", got)
	}
	unborn := testenv.TempDir(t)
	if out, err := exec.Command("git", "-C", unborn, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if got := Inspect(context.Background(), opened(t, unborn)); got.Available || !strings.Contains(got.Reason, "initial commit") {
		t.Fatalf("unborn availability = %+v", got)
	}
}

func TestInspectWithoutGitExplainsSafeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PATH", testenv.TempDir(t))
	} else {
		t.Setenv("PATH", testenv.TempDir(t))
	}
	got := Inspect(context.Background(), opened(t, testenv.TempDir(t)))
	if got.Available || !strings.Contains(got.Reason, "Git is not installed") || !strings.Contains(got.Reason, "serialize writes") {
		t.Fatalf("no-Git availability = %+v", got)
	}
}

func TestManagedPathBoundary(t *testing.T) {
	managed := filepath.Join(testenv.TempDir(t), "worktrees")
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
// now lives in internal/platform/gitcmd and is covered by that package's tests.

func TestGitWorktreeAddUsesExtendedTimeout(t *testing.T) {
	if got := gitTimeout([]string{"status", "--porcelain=v1"}); got != gitProbeTimeout {
		t.Fatalf("status timeout = %v, want %v", got, gitProbeTimeout)
	}
	if got := gitTimeout([]string{"reset", "--hard", "HEAD"}); got != gitWorktreeAddTimeout {
		t.Fatalf("reset timeout = %v, want %v: the new worktree's checkout happens there", got, gitWorktreeAddTimeout)
	}
	got := gitTimeout([]string{"worktree", "add", "-b", "branch", "destination", "HEAD"})
	if got != gitWorktreeAddTimeout {
		t.Fatalf("worktree add timeout = %v, want %v", got, gitWorktreeAddTimeout)
	}
	if got < 2*time.Minute || got <= gitProbeTimeout {
		t.Fatalf("worktree add timeout = %v, want a checkout-safe timeout longer than probes", got)
	}
}

func TestCreateSupportsPathsWithSpaces(t *testing.T) {
	requireGit(t)
	parent := testenv.TempDir(t)
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
	result, err := Create(context.Background(), opened(t, repo), filepath.Join(parent, "managed worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && !strings.Contains(result.WorktreeRoot, "managed worktrees") {
		t.Fatalf("unexpected worktree root %q", result.WorktreeRoot)
	}
}
