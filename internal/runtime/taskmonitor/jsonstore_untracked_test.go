package taskmonitor

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// gitInit makes dir a fresh repository, so its status is what a user would see.
// The machine's git config and ignore files are cut off: a global excludes
// file that already ignores .reasonix would pass this for the wrong reason.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(testenv.TempDir(t), "none"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", testenv.TempDir(t))
	if out, err := git(dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func git(dir string, args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", dir, "-c", "core.excludesFile="}, args...)...)
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := git(dir, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestFileStoreStateStaysOutOfGitStatus(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	writes := map[string]func(store *FileStore, dir string) error{
		"task": func(store *FileStore, dir string) error {
			return store.SaveTask(ctx, dir, TaskSnapshot{
				SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
				State: TaskStateRunning, Version: 1, CreatedAt: now, UpdatedAt: now,
			})
		},
		"idempotency": func(store *FileStore, dir string) error {
			return store.RecordIdempotency(ctx, dir, IdempotencyRecord{Key: "k1", Op: "stop", TaskID: "t1", Version: 1})
		},
		"claim": func(store *FileStore, dir string) error {
			_, err := store.ClaimIdempotency(ctx, dir, IdempotencyRecord{Key: "k2", Op: "stop", TaskID: "t1", Version: 1})
			return err
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			dir := testenv.TempDir(t)
			gitInit(t, dir)
			if err := write(NewFileStore(".reasonix/tasks"), dir); err != nil {
				t.Fatal(err)
			}
			if got := gitStatus(t, dir); got != "" {
				t.Fatalf("task state shows up as a change to commit:\n%s", got)
			}
		})
	}
}
