package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

func TestParseGitNumstat(t *testing.T) {
	added, removed := parseGitNumstat("10\t2\ta.go\n-\t-\tasset.bin\n3\t0\tpath with spaces.go\n")
	if added != 13 || removed != 2 {
		t.Fatalf("parseGitNumstat = (+%d -%d), want (+13 -2)", added, removed)
	}
}

func TestCountUntracked(t *testing.T) {
	got := countUntracked(" M tracked.go\n?? new.go\n?? nested/\n")
	if got != 2 {
		t.Fatalf("countUntracked = %d, want 2", got)
	}
}

func TestRunGitDisablesOptionalLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script fake git")
	}

	bin := filepath.Join(testenv.TempDir(t), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGit := filepath.Join(bin, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nprintf '%s' \"$GIT_OPTIONAL_LOCKS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := testenv.TempDir(t)
	repo := gitcmd.Repo{Dir: dir, GitDir: dir, CommonDir: dir, WorkTree: dir}
	out, err := runGit(context.Background(), repo, "status")
	if err != nil {
		t.Fatalf("runGit: %v", err)
	}
	if out != "0" {
		t.Fatalf("GIT_OPTIONAL_LOCKS = %q, want 0", out)
	}
}
