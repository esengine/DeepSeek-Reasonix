package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gitRepo builds a repository with one commit, which is the state `git diff
// HEAD` needs to answer at all.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir, err := os.MkdirTemp("", "repolint-changed")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	// git leaves read-only objects behind on Windows, where RemoveAll then
	// fails; a leaked temp directory is cheaper than a flaky suite.
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	write(t, dir, "kept.go", "package p\n\nfunc Kept() {}\n")
	write(t, dir, "gone.go", "package p\n\nfunc Gone() {}\n")
	git(t, dir, "init", "--quiet")
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.email=t@example.invalid", "-c", "user.name=t",
		"commit", "--quiet", "-m", "base")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// The witness this fix has: a file the working tree added but git has not been
// told about is invisible to `git diff`, so the universe built from it alone
// leaves the file unscanned while the run still reports clean.
func TestChangedUniverseIncludesUntrackedFiles(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "kept.go", "package p\n\nfunc Kept() int { return 1 }\n")
	write(t, dir, "added.go", "package p\n\nfunc Added() {}\n")

	tracked, err := trackedChangedPaths(dir)
	if err != nil {
		t.Fatalf("tracked: %v", err)
	}
	if !slices.Contains(tracked, "kept.go") {
		t.Fatalf("the modified file is missing from the tracked universe: %v", tracked)
	}
	if slices.Contains(tracked, "added.go") {
		t.Fatal("git diff started reporting untracked files; this test no longer witnesses the hole it was written for")
	}

	universe := changedUniverse(t, dir)
	for _, want := range []string{"kept.go", "added.go"} {
		if !universe[want] {
			t.Fatalf("%s is not in the changed universe: %v", want, universe)
		}
	}
}

// The consequence, stated as the gate states it: one finding, two universes,
// two verdicts. Under the old universe the run reports clean over a file it
// never opened, which is a green that means nothing.
func TestNarrowedCheckOverTheTrackedUniverseHidesAnUntrackedFinding(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "added.go", "package p\n\nfunc Added() {}\n")

	finding := []Finding{{File: "added.go", Line: 1, Rule: ruleErrorText, Msg: "witness", Weight: 1}}
	overrun := []Overrun{{File: "added.go", Rule: ruleErrorText}}

	tracked, err := trackedChangedPaths(dir)
	if err != nil {
		t.Fatalf("tracked: %v", err)
	}
	old := splitPaths(joinChangedPaths(tracked))
	if _, hidden := limitToPaths(finding, overrun, old); len(hidden) != 0 {
		t.Fatal("the tracked-only universe kept the finding; the false green this test guards is gone")
	}

	_, kept := limitToPaths(finding, overrun, changedUniverse(t, dir))
	if len(kept) != 1 {
		t.Fatalf("the changed universe dropped the untracked file's overrun: %v", kept)
	}
}

// A path the change removed is not scannable, so it stays out of the universe
// rather than being handed to a scanner that would fail on its absence.
func TestChangedUniverseExcludesDeletedPaths(t *testing.T) {
	dir := gitRepo(t)
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if universe := changedUniverse(t, dir); universe["gone.go"] {
		t.Fatalf("a deleted path entered the universe: %v", universe)
	}
}

func changedUniverse(t *testing.T, dir string) map[string]bool {
	t.Helper()
	list, err := changedPaths(dir)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	return splitPaths(list)
}
