package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func hasConfig(args []string, want string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && args[i+1] == want {
			return true
		}
	}
	return false
}

func TestArgsCarryBaselineConfig(t *testing.T) {
	args := argsFor("linux", "/repo", nil, "status", "--porcelain=v1")
	for _, want := range []string{"core.fsmonitor=false", "maintenance.auto=false"} {
		if !hasConfig(args, want) {
			t.Fatalf("args = %v, want -c %s", args, want)
		}
	}
	if i := slices.Index(args, "-C"); i < 0 || args[i+1] != "/repo" {
		t.Fatalf("args = %v, want -C /repo", args)
	}
	// Caller arguments stay last and in order.
	if got := args[len(args)-2:]; got[0] != "status" || got[1] != "--porcelain=v1" {
		t.Fatalf("trailing args = %v, want the caller's arguments last", got)
	}
}

// Extra config may add to the baseline but must never replace it: a call site
// that wants its own preference still gets the hardening.
func TestArgsExtraConfigCannotDropBaseline(t *testing.T) {
	args := argsFor("linux", "/repo", []string{"core.quotepath=false", ""}, "status")
	if !hasConfig(args, "core.fsmonitor=false") {
		t.Fatalf("args = %v, want the baseline retained alongside extra config", args)
	}
	if !hasConfig(args, "core.quotepath=false") {
		t.Fatalf("args = %v, want the extra config applied", args)
	}
	base := slices.Index(args, "core.fsmonitor=false")
	extra := slices.Index(args, "core.quotepath=false")
	if base > extra {
		t.Fatalf("args = %v, want baseline before extra config so the caller's value wins ties", args)
	}
	if slices.Contains(args, "") {
		t.Fatalf("args = %v, want empty config entries dropped", args)
	}
}

func TestArgsEnableLongPathsOnlyOnWindows(t *testing.T) {
	if args := argsFor("windows", `C:\Users\test\repo`, nil, "status"); !hasConfig(args, "core.longpaths=true") {
		t.Fatalf("windows args = %v, want core.longpaths=true", args)
	}
	if args := argsFor("linux", "/tmp/repo", nil, "status"); hasConfig(args, "core.longpaths=true") {
		t.Fatalf("non-windows args = %v, must not override core.longpaths", args)
	}
}

// diff is the one subcommand that can be pointed at an external program by
// repository configuration, so it carries the disabling flags — placed after
// the subcommand, never duplicated, and never added to other subcommands.
func TestDiffDisablesRepositoryConfiguredPrograms(t *testing.T) {
	args := argsFor("linux", "/repo", nil, "diff", "--numstat", "HEAD", "--")
	sub := slices.Index(args, "diff")
	if sub < 0 {
		t.Fatalf("args = %v, want the diff subcommand", args)
	}
	for _, flag := range []string{"--no-ext-diff", "--no-textconv"} {
		i := slices.Index(args, flag)
		if i < 0 {
			t.Fatalf("args = %v, want %s", args, flag)
		}
		if i < sub {
			t.Fatalf("args = %v, want %s after the subcommand", args, flag)
		}
	}
	if got := args[len(args)-3:]; got[0] != "--numstat" || got[1] != "HEAD" || got[2] != "--" {
		t.Fatalf("trailing args = %v, want the caller's diff arguments preserved in order", got)
	}

	explicit := argsFor("linux", "/repo", nil, "diff", "--no-ext-diff", "HEAD")
	if n := strings.Count(strings.Join(explicit, " "), "--no-ext-diff"); n != 1 {
		t.Fatalf("args = %v, want one --no-ext-diff when the caller already passed it", explicit)
	}

	if args := argsFor("linux", "/repo", nil, "status"); slices.Contains(args, "--no-ext-diff") {
		t.Fatalf("status args = %v, must not carry diff-only flags", args)
	}
}

func TestEnvDisablesPromptsAndKeepsSSHUsable(t *testing.T) {
	env := Env()
	if !slices.Contains(env, "GIT_OPTIONAL_LOCKS=0") || !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("env = %v, want optional locks and terminal prompts disabled", env)
	}
	// An empty value is a *present* value to git: clearing these would break
	// legitimate ssh remotes and external diff tooling rather than harden.
	for _, banned := range []string{"GIT_SSH_COMMAND=", "GIT_EXTERNAL_DIFF="} {
		if slices.Contains(env, banned) {
			t.Fatalf("env = %v, must not set %q", env, banned)
		}
	}
}

// The invariant this package exists for: a repository's own config names a
// command in core.fsmonitor, and inspecting that repository must not run it.
// git executes fsmonitor during an index refresh, which a plain status does.
func TestRepositoryConfigCannotRunCommandsDuringInspection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	repo := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run := func(args ...string) {
		t.Helper()
		if out, err := Command(ctx, repo, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "--quiet", "-m", "initial")

	// A repository whose config points fsmonitor at a command. Writing the
	// marker is what an attacker's payload would do first.
	marker := filepath.Join(t.TempDir(), "executed")
	payload := filepath.Join(t.TempDir(), "payload.sh")
	script := "#!/bin/sh\ntouch " + marker + "\nexit 1\n"
	if err := os.WriteFile(payload, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	run("config", "core.fsmonitor", payload)

	// Dirty the tree so an index refresh has work to do, then inspect it the
	// way the status readout does.
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = Command(ctx, repo, "status", "--porcelain=v1").CombinedOutput()
	_, _ = Command(ctx, repo, "diff", "--numstat", "HEAD", "--").CombinedOutput()
	_, _ = Command(ctx, repo, "rev-parse", "--show-toplevel").CombinedOutput()

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repository config ran a command during inspection")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
}

// The clean-filter residual from the advisory: a .gitattributes entry plus a
// filter.<driver>.clean command in the repository's local config makes
// `git diff` run that command to produce the "clean" working-tree side, and
// neither --no-ext-diff nor --no-textconv covers it. Diff invocations must
// neutralize every locally-defined driver while still rendering a correct
// diff (the emptied filter is an identity pass-through, not a content wipe).
func TestDiffDoesNotRunRepositoryCleanFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	repo := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run := func(args ...string) {
		t.Helper()
		if out, err := Command(ctx, repo, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	marker := filepath.Join(t.TempDir(), "executed")
	payload := filepath.Join(t.TempDir(), "clean.sh")
	script := "#!/bin/sh\ntouch " + marker + "\ncat\n"
	if err := os.WriteFile(payload, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("secret.bin filter=pwn\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "secret.bin"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".gitattributes", "secret.bin")
	run("commit", "--quiet", "-m", "initial")
	run("config", "filter.pwn.clean", payload)
	run("config", "filter.pwn.process", payload)
	run("config", "filter.pwn.required", "true")
	if err := os.WriteFile(filepath.Join(repo, "secret.bin"), []byte("secret\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The exact shape desktop/workspace_changes.go builds: -C inside args.
	out, err := Command(ctx, "", "-C", repo, "diff", "--no-ext-diff", "--no-textconv", "--relative", "HEAD", "--", filepath.FromSlash("secret.bin")).CombinedOutput()
	if err != nil {
		t.Fatalf("diff failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "changed") {
		t.Fatalf("diff output lost the working-tree change (filter neutralization must pass content through):\n%s", out)
	}
	// And the shape internal/cli/gitstatus.go builds: dir parameter + diff.
	if out, err = Command(ctx, repo, "diff", "--numstat", "HEAD", "--").CombinedOutput(); err != nil {
		t.Fatalf("numstat diff failed: %v: %s", err, out)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repository clean filter ran during a diff")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
}

func TestFilterNeutralizingConfigOnlyForDiff(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := `[core]
	bare = false
[filter "lfs"]
	clean = git-lfs clean -- %f
	process = git-lfs filter-process
	required = true
[filter "pwn"]
	smudge = whatever
`
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"filter.lfs.clean=", "filter.lfs.process=", "filter.lfs.required=false",
		"filter.pwn.clean=", "filter.pwn.process=", "filter.pwn.required=false",
	} {
		args := argsFor("linux", repo, nil, "diff", "HEAD")
		if !hasConfig(args, want) {
			t.Fatalf("diff args = %v, want -c %s", args, want)
		}
	}

	// The -C-inside-args form workspace_changes.go uses resolves the same repo.
	args := argsFor("linux", "", nil, "-C", repo, "diff", "--no-ext-diff")
	if !hasConfig(args, "filter.lfs.clean=") {
		t.Fatalf("args with -C inside = %v, want filter.lfs.clean= override", args)
	}

	// Non-diff subcommands carry no filter overrides, and a repo without
	// filter sections adds nothing even for diff.
	for _, sub := range []string{"status", "rev-parse", "diff-tree"} {
		if args := argsFor("linux", repo, nil, sub, "--porcelain=v1"); hasConfig(args, "filter.lfs.clean=") {
			t.Fatalf("%s args = %v, must not carry diff-only filter overrides", sub, args)
		}
	}
	clean := t.TempDir()
	if err := os.MkdirAll(filepath.Join(clean, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clean, ".git", "config"), []byte("[core]\n\tbare = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if args := argsFor("linux", clean, nil, "diff"); slices.Contains(args, "filter.") {
		t.Fatalf("filter-free repo diff args = %v, want no filter overrides", args)
	}
}

// A linked worktree keeps its config next to the gitdir the .git file points
// at; the neutralization must follow the link.
func TestLocalFilterDriversFollowsWorktreeLink(t *testing.T) {
	main := t.TempDir()
	if err := os.MkdirAll(filepath.Join(main, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(main, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".git", "config"), []byte("[filter \"x\"]\n\tclean = cmd\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := localFilterDrivers(wt); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("localFilterDrivers(worktree) = %v, want [x]", got)
	}
	args := argsFor("linux", wt, nil, "diff")
	if !hasConfig(args, "filter.x.clean=") {
		t.Fatalf("worktree diff args = %v, want filter.x.clean= override", args)
	}
}

func TestGitSubcommandSkipsGlobalOptions(t *testing.T) {
	for _, tt := range []struct {
		args []string
		sub  string
		cDir string
	}{
		{args: []string{"status"}, sub: "status"},
		{args: []string{"diff", "HEAD"}, sub: "diff"},
		{args: []string{"-C", "/repo", "diff"}, sub: "diff", cDir: "/repo"},
		{args: []string{"-C/repo", "status"}, sub: "status", cDir: "/repo"},
		{args: []string{"-c", "a=b", "-C", "/r", "log"}, sub: "log", cDir: "/r"},
		{args: []string{"--no-pager", "status"}, sub: "status"},
		{args: []string{}},
		{args: []string{"-c", "a=b"}},
	} {
		sub, cDir := gitSubcommand(tt.args)
		if sub != tt.sub || cDir != tt.cDir {
			t.Fatalf("gitSubcommand(%v) = (%q, %q), want (%q, %q)", tt.args, sub, cDir, tt.sub, tt.cDir)
		}
	}
}
