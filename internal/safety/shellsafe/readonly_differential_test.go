package shellsafe

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// For every generated command the classifier accepts as a read, run it in a
// scratch directory and compare the directory before and after: any change is
// a command called a read that wrote.

var differentialPrograms = []string{
	"cat", "head", "tail", "ls", "find", "grep", "egrep", "fgrep", "echo", "printf", "pwd",
	"wc", "sort", "uniq", "cut", "tr", "stat", "file", "du", "diff", "cmp", "comm",
	"basename", "dirname", "realpath", "readlink", "env", "true", "false", "test",
}

var differentialArgs = []string{
	"a.txt", "b.txt", "out.txt", "new.txt", "sub", "sub/c.txt", ".", "-", "--",
	"-n", "-l", "-r", "-i", "-v", "-c", "-d", "-u", "-f", "-o", "-s", "-t", "-e", "-x", "-a", "-z", "-1", "-2", "-3",
	"-delete", "-fprint", "-fprintf", "-fls", "-exec", "-execdir", "-ok", "-okdir", "-name", "-type", "-print", "-print0", "-prune",
	"--output=out.txt", "--output", "--compress-program=sh", "--files0-from=a.txt", "--help", "--version", "--color=always",
	"rm", "touch", "mv", "cp", "sh", "-c", "{}", ";", "+", "tee", "x", "1", "2", "pattern", "'a b'", `"a.txt"`,
}

func snapshotDir(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		line := fmt.Sprintf("%s|%v|%d", rel, info.Mode(), info.Size())
		if info.Mode().IsRegular() {
			data, readErr := os.ReadFile(path)
			if errors.Is(readErr, fs.ErrNotExist) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			line += fmt.Sprintf("|%x", sha256.Sum256(data))
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func seedScratch(t *testing.T, dir string) {
	t.Helper()
	for name, body := range map[string]string{"a.txt": "alpha\nbeta\nalpha\n", "b.txt": "beta\ngamma\n", "sub/c.txt": "pattern here\n"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEveryCommandTheClassifierCallsAReadLeavesTheDirectoryAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process per generated command")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the generated commands are POSIX shell commands")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	present := map[string]bool{}
	for _, program := range differentialPrograms {
		if _, err := exec.LookPath(program); err == nil || program == "echo" || program == "printf" || program == "test" || program == "true" || program == "false" {
			present[program] = true
		}
	}
	rng := rand.New(rand.NewSource(20261016))
	scratch := testenv.TempDir(t)
	home := testenv.TempDir(t)
	classified, executed := 0, 0
	seen := map[string]bool{}
	for range 1200 {
		program := differentialPrograms[rng.Intn(len(differentialPrograms))]
		if !present[program] {
			continue
		}
		parts := []string{program}
		for n := rng.Intn(5); n > 0; n-- {
			parts = append(parts, differentialArgs[rng.Intn(len(differentialArgs))])
		}
		command := strings.Join(parts, " ")
		if seen[command] {
			continue
		}
		seen[command] = true
		base, sub, fields, ok := ClassifyReadOnlyCommand(command)
		if !ok || ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
			continue
		}
		classified++
		if err := os.RemoveAll(scratch); err != nil {
			t.Fatal(err)
		}
		seedScratch(t, scratch)
		before := snapshotDir(t, scratch)
		cmd := exec.Command(sh, "-c", command)
		cmd.Dir = scratch
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
		cmd.Stdin = nil
		done := make(chan struct{})
		go func() {
			_ = cmd.Run()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		executed++
		if after := snapshotDir(t, scratch); after != before {
			t.Errorf("classified as a read but changed the directory: %q\nbefore:\n%s\nafter:\n%s", command, before, after)
		}
	}
	if classified < 30 {
		t.Fatalf("only %d generated commands were classified as reads; the generator no longer exercises the table", classified)
	}
	t.Logf("%d commands classified as reads, %d executed", classified, executed)
}

var gitDifferentialSubs = []string{"branch", "remote", "config", "stash", "merge-base", "log", "diff", "show", "tag", "status", "rev-parse", "describe", "reflog", "shortlog", "blame", "grep", "ls-files", "ls-tree", "cat-file", "for-each-ref", "name-rev", "rev-list", "cherry", "whatchanged"}

var gitDifferentialArgs = []string{
	"main", "HEAD", "HEAD~1", "topic", "newbranch", "v1", "origin", "a.txt", "-", "--",
	"-a", "-r", "-v", "-vv", "-l", "--list", "-d", "-D", "-m", "-M", "-c", "-C", "-f", "-u", "-t", "-n", "-p", "-s", "-e", "-z",
	"--delete", "--move", "--copy", "--force", "--set-upstream-to=main", "--unset-upstream", "--edit-description", "--track", "--show-current",
	"--contains", "--merged", "--no-merged", "--points-at", "--sort=-committerdate", "--format=%(refname)", "--oneline", "--stat", "--all",
	"add", "remove", "rm", "rename", "set-url", "set-head", "prune", "update", "get-url", "show",
	"--get", "--get-all", "--get-regexp", "--list", "--global", "--local", "--system", "--unset", "--add", "--replace-all", "--file=x.cfg", "-f=x.cfg",
	"user.name", "core.bare", "alias.x", "x.y", "Bob", "!sh",
	"push", "pop", "apply", "drop", "clear", "save", "branch", "create", "store",
	"--output=out.txt", "--ext-diff", "--textconv", "-O", "--open-files-in-pager",
}

func TestEveryGitCommandTheClassifierCallsAReadLeavesTheRepositoryAlone(t *testing.T) {
	if testing.Short() || runtime.GOOS == "windows" {
		t.Skip("spawns a process per generated command")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	sh, _ := exec.LookPath("sh")
	home := testenv.TempDir(t)
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LC_ALL=C", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_EDITOR=true"}
	build := func(dir string) {
		t.Helper()
		seedScratch(t, dir)
		for _, step := range [][]string{
			{"init", "-q", "-b", "main"}, {"config", "gc.auto", "0"}, {"config", "maintenance.auto", "false"}, {"config", "user.email", "t@example.test"}, {"config", "user.name", "T"},
			{"add", "."}, {"commit", "-q", "-m", "one"}, {"branch", "topic"}, {"tag", "v1"},
		} {
			cmd := exec.Command(git, step...)
			cmd.Dir, cmd.Env = dir, env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", step, err, out)
			}
		}
	}
	snapshot := func(dir string) string {
		full := snapshotDir(t, dir)
		var keep []string
		for line := range strings.SplitSeq(full, "\n") {
			if strings.HasPrefix(line, ".git/index") || strings.Contains(line, ".lock|") {
				continue
			}
			keep = append(keep, line)
		}
		return strings.Join(keep, "\n")
	}
	rng := rand.New(rand.NewSource(20261017))
	scratch := testenv.TempDir(t)
	classified := 0
	seen := map[string]bool{}
	for range 120 {
		sub := gitDifferentialSubs[rng.Intn(len(gitDifferentialSubs))]
		parts := []string{"git", sub}
		for n := rng.Intn(4); n > 0; n-- {
			parts = append(parts, gitDifferentialArgs[rng.Intn(len(gitDifferentialArgs))])
		}
		command := strings.Join(parts, " ")
		if seen[command] {
			continue
		}
		seen[command] = true
		base, subWord, fields, ok := ClassifyReadOnlyCommand(command)
		if !ok || ArgsMakeReadOnlyCommandWrite(base, subWord, fields) {
			continue
		}
		classified++
		if err := os.RemoveAll(scratch); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(scratch, 0o755); err != nil {
			t.Fatal(err)
		}
		build(scratch)
		before := snapshot(scratch)
		cmd := exec.Command(sh, "-c", command)
		cmd.Dir, cmd.Env = scratch, env
		done := make(chan struct{})
		go func() {
			_ = cmd.Run()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if after := snapshot(scratch); after != before {
			t.Errorf("classified as a read but changed the repository: %q\nbefore:\n%s\nafter:\n%s", command, before, after)
		}
	}
	if classified < 10 {
		t.Fatalf("only %d generated git commands were classified as reads", classified)
	}
	t.Logf("%d git commands classified as reads and executed", classified)
}
