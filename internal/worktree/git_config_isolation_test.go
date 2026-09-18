package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostileHooksPath builds the machine that made the hook tests destructive: a
// user-level git config pointing core.hooksPath at a directory the developer
// owns. The hook planted there records that it ran and exits 0, so it changes
// nothing except leave evidence - a hook that failed would be indistinguishable
// from an unrelated broken commit.
//
// Returns the hook's path and the marker it writes when git runs it.
func hostileHooksPath(t *testing.T) (hook, marker string) {
	t.Helper()
	dir := t.TempDir()
	hook = filepath.Join(dir, "pre-commit")
	marker = filepath.Join(t.TempDir(), "ambient-hook-ran")
	script := "#!/bin/sh\nprintf ran > '" + marker + "'\nexit 0\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[core]\n\thooksPath = "+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set before requireGit, exactly as the ambient environment would be, so
	// the isolation has to win rather than merely arrive first.
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	return hook, marker
}

// The write side. Any test that asks git where the pre-commit hook lives and
// then writes one must be answered with a path inside its own repository. This
// asserts the property rather than the two call sites that rely on it, so a
// third one cannot be added outside its cover.
func TestHookPathResolvesInsideTheTestRepository(t *testing.T) {
	hook, _ := hostileHooksPath(t)
	before, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}

	requireGit(t)
	repo := initRepo(t)

	resolved := gitTest(t, repo, "rev-parse", "--git-path", "hooks/pre-commit")
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(repo, resolved)
	}
	if !strings.HasPrefix(resolved, repo) {
		t.Fatalf("hook path resolved outside the test repository: %s", resolved)
	}

	after, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("the developer's hook is gone: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the developer's hook was rewritten:\n%s", after)
	}
}

// The read side, and the reason the hook tests could pass for the wrong reason.
// The package spawns git through runGit, which inherits os.Environ(), so the
// isolation has to reach the code under test and not only the helpers: a hook
// configured outside the repository must not run during the commits these
// tests make.
func TestCodeUnderTestDoesNotRunAnAmbientHook(t *testing.T) {
	_, marker := hostileHooksPath(t)

	requireGit(t)
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := runGit(context.Background(), repo, "add", "change.txt"); err != nil {
		t.Fatalf("git add: %v%s", err, stderrSuffix(stderr))
	}
	if _, stderr, err := runGit(context.Background(), repo,
		"-c", "user.name=Reasonix Test", "-c", "user.email=reasonix@example.invalid",
		"commit", "-m", "change"); err != nil {
		t.Fatalf("git commit: %v%s", err, stderrSuffix(stderr))
	}

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a hook outside the repository ran during the test's own commit, stat err = %v", err)
	}
}
