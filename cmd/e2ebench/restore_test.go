package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withFakeIO(t *testing.T, w func(string, []byte, os.FileMode) error, r func(string) ([]byte, error)) {
	t.Helper()
	ow, or := writeFile, readFile
	writeFile, readFile = w, r
	t.Cleanup(func() { writeFile, readFile = ow, or })
}

// A restore that cannot write is not a restore.
func TestRestoreSourceFailsWhenTheWriteFails(t *testing.T) {
	withFakeIO(t, func(string, []byte, os.FileMode) error { return errors.New("disk full") }, os.ReadFile)
	err := restoreSource("/tmp/unused", []byte("original"))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("a failed restore write was not reported: %v", err)
	}
}

// A restore that wrote something else is not a restore either.
func TestRestoreSourceFailsWhenTheBytesDoNotComeBack(t *testing.T) {
	withFakeIO(t,
		func(string, []byte, os.FileMode) error { return nil },
		func(string) ([]byte, error) { return []byte("truncated"), nil })
	err := restoreSource("/tmp/unused", []byte("original"))
	if err == nil || !strings.Contains(err.Error(), "bytes on disk") {
		t.Fatalf("a silent truncation passed verification: %v", err)
	}
}

// The real filesystem is the other witness: writing over a directory fails.
func TestRestoreSourceFailsOnARealWriteError(t *testing.T) {
	if err := restoreSource(t.TempDir(), []byte("original")); err == nil {
		t.Fatal("restoring onto a directory reported success")
	}
}

func TestTreeMatchesSeesADirtyTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	file := "a.go"
	if err := os.WriteFile(filepath.Join(repo, file), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", file)
	git("commit", "-qm", "init")

	if err := treeMatches(repo, "HEAD", []string{file}); err != nil {
		t.Fatalf("a clean tree was reported dirty: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, file), []byte("package a // mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := treeMatches(repo, "HEAD", []string{file}); err == nil {
		t.Fatal("a mutated file passed the tree check")
	}
}

// The mutation sweep must not hand back a caught/survivor verdict when the
// source could not be restored: the numbers would describe a tree that no
// longer holds the PR's code.
func TestRunMutationFailsWhenTheRestoreFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	mustWrite := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", "module mutationfixture\n\ngo 1.24\n")
	mustWrite("pkg/mut.go", "package pkg\n\nfunc Answer() int {\n\treturn 1\n}\n")
	mustWrite("pkg/mut_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestMut(t *testing.T) {\n\tif Answer() != 2 {\n\t\tt.Fatalf(\"got %d\", Answer())\n\t}\n}\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")

	mustWrite("pkg/mut.go", "package pkg\n\nfunc Answer() int {\n\treturn 2\n}\n")
	git("add", "-A")
	git("commit", "-qm", "change")

	// The first write mutates the source, the second restores it. Failing the
	// second one is exactly the case the sweep used to swallow.
	calls := 0
	withFakeIO(t, func(path string, body []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return errors.New("disk full")
		}
		return os.WriteFile(path, body, mode)
	}, os.ReadFile)

	res, err := runMutation(repo, base, []string{"pkg/mut.go"}, []testRef{{name: "TestMut", pkg: "./pkg"}})
	if err == nil {
		t.Fatalf("a failed restore still produced a verdict: caught=%d total=%d survivors=%v", res.caught, res.total, res.survivors)
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error does not name the failing restore: %v", err)
	}
}

// The same guarantee on the differential side: a restore that cannot put a file
// back must stop the analysis instead of reporting pins from a mixed tree.
func TestDifferentialPerTestFailsWhenRestoreFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module difffixture\n\ngo 1.24\n")
	write("pkg/mut.go", "package pkg\n\nfunc Answer() int {\n\treturn 1\n}\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")

	write("pkg/mut.go", "package pkg\n\nfunc Answer() int {\n\treturn 2\n}\n")
	git("add", "-A")
	git("commit", "-qm", "change")

	// pkg/missing.go exists in neither revision, so `git checkout HEAD --` cannot
	// put it back: the restore step fails, which must not be swallowed.
	srcFiles := []string{"pkg/mut.go", "pkg/missing.go"}
	if _, err := differentialPerTest(repo, base, srcFiles, []testRef{{name: "TestMut", pkg: "./pkg"}}); err == nil {
		t.Fatal("a failed restore still produced pin results")
	}
}
