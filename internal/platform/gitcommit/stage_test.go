package gitcommit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitstatus"
)

func statusOf(t *testing.T, dir string) map[string]gitstatus.Change {
	t.Helper()
	repo := reopen(t, dir)
	changes, ok, err := gitstatus.Status(context.Background(), repo)
	if err != nil || !ok {
		t.Fatalf("status: ok=%v err=%v", ok, err)
	}
	out := map[string]gitstatus.Change{}
	for _, c := range changes {
		out[c.Path] = c
	}
	return out
}

func TestStagingATrackedEditAnUntrackedFileAndADeletion(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "a.go", "package a\n// edited\n")
	write(t, dir, "new.go", "package n\n")
	write(t, dir, "gone.go", "package g\n")
	run(t, dir, "add", "gone.go")
	run(t, dir, "commit", "-qm", "gone")
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if err := Stage(context.Background(), repo, []string{"a.go", "new.go", "gone.go"}, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a.go", "new.go", "gone.go"} {
		if c := statusOf(t, dir)[path]; !c.Staged || c.Unstaged {
			t.Errorf("%s after staging: %+v", path, c)
		}
	}
}

func TestStageAllStagesEveryListedChangeAndLeavesNestedRepositoriesAlone(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "a.go", "package a\n// edited\n")
	write(t, dir, "b.go", "package b\n")
	nested := filepath.Join(dir, "vendor", "lib")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	run(t, nested, "init", "-q")
	write(t, nested, "x.go", "package x\n")
	if err := Stage(context.Background(), repo, nil, true); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t, dir)
	if !st["a.go"].Staged || !st["b.go"].Staged {
		t.Fatalf("the files were not staged: %+v", st)
	}
	staged := run(t, dir, "diff", "--cached", "--name-only")
	if strings.Contains(staged, "vendor") {
		t.Fatalf("a nested repository was staged: %s", staged)
	}
}

func TestUnstagingRestoresTheIndexAndKeepsTheWork(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "a.go", "package a\n// edited\n")
	write(t, dir, "b.go", "package b\n")
	run(t, dir, "add", "a.go", "b.go")
	if err := Unstage(context.Background(), repo, []string{"a.go", "b.go"}, false); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t, dir)
	if st["a.go"].Staged || !st["a.go"].Unstaged || st["b.go"].Staged || st["b.go"].Status != "??" {
		t.Fatalf("after unstaging: %+v", st)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "a.go")); !strings.Contains(string(body), "edited") {
		t.Fatal("unstaging touched the work tree")
	}
}

func TestUnstagingInARepositoryWithNoCommitYet(t *testing.T) {
	isolate(t)
	dir := testenv.TempDir(t)
	run(t, dir, "init", "-q")
	write(t, dir, "first.go", "package f\n")
	run(t, dir, "add", "first.go")
	if err := Unstage(context.Background(), reopen(t, dir), []string{"first.go"}, false); err != nil {
		t.Fatal(err)
	}
	if c := statusOf(t, dir)["first.go"]; c.Staged || c.Status != "??" {
		t.Fatalf("first.go: %+v", c)
	}
}

func TestUnstagingARenameReturnsBothHalves(t *testing.T) {
	dir, repo := fixture(t)
	run(t, dir, "mv", "a.go", "renamed.go")
	if err := Unstage(context.Background(), repo, []string{"renamed.go"}, false); err != nil {
		t.Fatal(err)
	}
	if staged := run(t, dir, "diff", "--cached", "--name-only"); strings.TrimSpace(staged) != "" {
		t.Fatalf("part of the rename is still staged: %s", staged)
	}
}

// A path is a literal name. A glob, a pathspec magic prefix or an option
// spelling must neither match other files nor reach git as anything but a path.
func TestOnlyChangedPathsAreAcceptedAndTheyAreLiteral(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "one.txt", "1\n")
	write(t, dir, "two.txt", "2\n")
	// As a glob this name matches one.txt and two.txt; it avoids * and ?, which
	// a Windows file name cannot hold.
	const glob = "[ot][nw][eo].txt"
	write(t, dir, glob, "glob\n")
	if err := Stage(context.Background(), repo, []string{glob}, false); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t, dir)
	if !st[glob].Staged || st["one.txt"].Staged || st["two.txt"].Staged {
		t.Fatalf("a glob matched files it did not name: %+v", st)
	}
	for _, bad := range []string{"../outside.txt", "/etc/passwd", "--all", ":(top)a.go", "missing.txt", ""} {
		if err := Stage(context.Background(), repo, []string{bad}, false); !errors.Is(err, ErrUnknownPath) {
			t.Errorf("%q: %v, want ErrUnknownPath", bad, err)
		}
	}
}

func TestStagingWithoutARepositoryOrAnythingToDo(t *testing.T) {
	if err := Stage(context.Background(), reopen(t, testenv.TempDir(t)), nil, true); !errors.Is(err, ErrNoRepository) {
		t.Fatalf("no repo: %v", err)
	}
	_, repo := fixture(t)
	if err := Stage(context.Background(), repo, nil, false); !errors.Is(err, ErrUnknownPath) {
		t.Fatalf("no paths and not all: %v", err)
	}
	if err := Stage(context.Background(), repo, nil, true); err != nil {
		t.Fatalf("stage all of a clean tree: %v", err)
	}
}
