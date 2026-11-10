package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestCreateCancellationStopsCheckoutDescendantsAndAllowsRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX shell; process cancellation has platform-specific tests")
	}
	requireGit(t)
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("README.md filter=wait\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := opened(t, root)
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "filter"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	fixture := testenv.TempDir(t)
	started, survived := filepath.Join(fixture, "started"), filepath.Join(fixture, "survived")
	script := filepath.Join(fixture, "smudge.sh")
	if err := os.WriteFile(script, []byte("printf ready > \"$1\"\nsleep 3\nprintf survived > \"$2\"\ncat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(fixture, "gitconfig")
	for key, value := range map[string]string{"filter.wait.clean": "cat", "filter.wait.smudge": fmt.Sprintf("sh %q %q %q", script, started, survived), "filter.wait.required": "true"} {
		if out, err := exec.Command("git", "config", "--file", global, key, value).CombinedOutput(); err != nil {
			t.Fatalf("config: %v %s", err, out)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	managedRoot := testenv.TempDir(t)
	go func() {
		result, err := CreateFromProgress(ctx, repo, managedRoot, "HEAD", nil)
		done <- outcome{result, err}
	}()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("creation exited before filter: %+v", got)
		case <-ctx.Done():
			t.Fatal("filter never started")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	got := <-done
	if !errors.Is(got.err, context.Canceled) || got.result.WorktreeRoot == "" {
		t.Fatalf("cancelled checkout did not return cleanup ownership: %+v", got)
	}
	if _, err := os.Stat(survived); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkout descendant survived cancellation: %v", err)
	}
	if err := DiscardUnpublished(t.Context(), repo, managedRoot, got.result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got.result.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial checkout retained: %v", err)
	}
}
