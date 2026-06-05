package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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

func TestGitStatusRender(t *testing.T) {
	got := ansi.Strip(gitStatus{Repo: "mySkills", Branch: "main", Added: 15}.Render())
	if got != "mySkills@main (+15 -0)" {
		t.Fatalf("Render = %q", got)
	}

	got = ansi.Strip(gitStatus{Repo: "repo", Branch: "abc1234", Detached: true, Untracked: 2}.Render())
	if got != "repo@abc1234 (?2)" {
		t.Fatalf("detached Render = %q", got)
	}
}

func TestGitStatusRenderRepoUsesSuppliedRepoStyle(t *testing.T) {
	got := ansi.Strip(gitStatus{Repo: "repo", Branch: "main"}.RenderRepo("[repo]"))
	if got != "[repo]@main" {
		t.Fatalf("RenderRepo = %q, want styled repo name only", got)
	}
}

func TestGitStatusRenderWithinCompactsRepoBeforeBranch(t *testing.T) {
	status := gitStatus{Repo: "VeryLongDeepSeekReasonixWorkspace", Branch: "codex/cli-tui-status-row"}

	full := ansi.Strip(status.RenderWithin(80, statusAutoColor))
	if full != "VeryLongDeepSeekReasonixWorkspace@codex/cli-tui-status-row" {
		t.Fatalf("wide RenderWithin = %q", full)
	}

	got := ansi.Strip(status.RenderWithin(46, statusAutoColor))
	if ansi.StringWidth(got) > 46 {
		t.Fatalf("compacted status width = %d, want <= 46: %q", ansi.StringWidth(got), got)
	}
	if !strings.Contains(got, "@codex/cli-tui-status-row") {
		t.Fatalf("branch should stay intact while repo can be compacted: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("long repo should be compacted with ellipsis: %q", got)
	}
}

func TestGitStatusRenderWithinKeepsDirtySuffix(t *testing.T) {
	status := gitStatus{
		Repo:      "VeryLongDeepSeekReasonixWorkspace",
		Branch:    "codex/cli-tui-status-row",
		Added:     12,
		Removed:   3,
		Untracked: 4,
	}

	got := ansi.Strip(status.RenderWithin(35, statusAutoColor))
	if ansi.StringWidth(got) > 35 {
		t.Fatalf("compacted dirty status width = %d, want <= 35: %q", ansi.StringWidth(got), got)
	}
	if !strings.Contains(got, "(+12 -3 ?4)") {
		t.Fatalf("dirty suffix should be preserved: %q", got)
	}
	if !strings.Contains(got, "@") || !strings.Contains(got, "…") {
		t.Fatalf("identity should remain segmented and compacted: %q", got)
	}
}

func TestLoadGitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	root := t.TempDir()
	runGitForTest(t, root, "init")
	runGitForTest(t, root, "config", "user.email", "reasonix@example.invalid")
	runGitForTest(t, root, "config", "user.name", "Reasonix Test")
	writeFileForTest(t, filepath.Join(root, "tracked.txt"), "one\ntwo\n")
	runGitForTest(t, root, "add", "tracked.txt")
	runGitForTest(t, root, "commit", "-m", "initial")
	runGitForTest(t, root, "branch", "-M", "main")

	writeFileForTest(t, filepath.Join(root, "tracked.txt"), "one\nchanged\nthree\n")
	writeFileForTest(t, filepath.Join(root, "new.txt"), "untracked\n")
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := loadGitStatus(ctx, filepath.Join(root, "subdir"))
	if err != nil {
		t.Fatalf("loadGitStatus: %v", err)
	}
	if status.Repo != filepath.Base(root) || status.Branch != "main" || status.Detached {
		t.Fatalf("status identity = %+v", status)
	}
	if status.Added != 2 || status.Removed != 1 || status.Untracked != 1 {
		t.Fatalf("status changes = (+%d -%d ?%d), want (+2 -1 ?1)", status.Added, status.Removed, status.Untracked)
	}
	if plain := ansi.Strip(status.Render()); !strings.Contains(plain, filepath.Base(root)+"@main") || !strings.Contains(plain, "+2 -1 ?1") {
		t.Fatalf("rendered status = %q", plain)
	}
}

func TestRunGitDisablesOptionalLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script fake git")
	}

	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGit := filepath.Join(bin, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nprintf '%s' \"$GIT_OPTIONAL_LOCKS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := runGit(context.Background(), "", "status")
	if err != nil {
		t.Fatalf("runGit: %v", err)
	}
	if out != "0" {
		t.Fatalf("GIT_OPTIONAL_LOCKS = %q, want 0", out)
	}
}

func runGitForTest(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFileForTest(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
