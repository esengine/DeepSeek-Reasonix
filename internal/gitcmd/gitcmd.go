// Package gitcmd builds the git invocations Reasonix runs on its own behalf:
// the status readout, workspace change probes, worktree management, and plugin
// source checkouts.
//
// Every one of those may point at a repository Reasonix did not create, and a
// repository's own .git/config is data authored by whoever produced the
// repository — not configuration the user chose. Several config keys name a
// command that git then executes during ordinary read-only work: an index
// refresh runs core.fsmonitor, a diff runs diff.external or a textconv driver,
// auto-maintenance spawns a background daemon. Command-line -c overrides win
// over repository config, and the corresponding --no-* flags win over both, so
// every invocation carries the same baseline.
//
// Centralizing that baseline is the point of this package. The same overrides
// used to be spelled out at each call site, which is exactly how three of the
// five sites ended up carrying them and two did not.
//
// Content filters (filter.<driver>.clean/process) are selected per driver name
// through .gitattributes, so diffs neutralize every driver defined in the
// repository's local .git/config instead (see filterNeutralizingConfig).
// include.path chains and core.sshCommand remain the user's own to vet.
package gitcmd

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// baseConfig is the -c override set every invocation carries.
var baseConfig = []string{
	// An index refresh (status, diff, rev-parse --show-toplevel in a dirty
	// tree) executes this as a command when the repository sets it.
	"core.fsmonitor=false",
	// Keeps a probe from starting git's background maintenance daemon.
	"maintenance.auto=false",
}

// Args returns the full argument list for a git invocation: the hardening
// overrides, an optional -C directory, then the caller's arguments. extraConfig
// entries are "key=value" pairs appended after the baseline, so a call site can
// add its own preferences but cannot drop the baseline.
func Args(dir string, extraConfig []string, args ...string) []string {
	return argsFor(runtime.GOOS, dir, extraConfig, args...)
}

func argsFor(goos, dir string, extraConfig []string, args ...string) []string {
	// No capacity hint: these lists are a handful of entries, and computing one
	// from the input lengths buys nothing measurable.
	var out []string
	for _, cfg := range baseConfig {
		out = append(out, "-c", cfg)
	}
	if goos == "windows" {
		out = append(out, "-c", "core.longpaths=true")
	}
	for _, cfg := range extraConfig {
		if cfg == "" {
			continue
		}
		out = append(out, "-c", cfg)
	}
	for _, cfg := range filterNeutralizingConfig(dir, args) {
		out = append(out, "-c", cfg)
	}
	if dir != "" {
		out = append(out, "-C", dir)
	}
	return append(out, hardenSubcommand(args)...)
}

// filterNeutralizingConfig returns -c overrides that blank every filter driver
// command the repository's local .git/config defines, but only when args runs a
// diff — the one gitcmd subcommand that invokes clean filters on working-tree
// content. A diff compares the file's raw bytes, so an emptied filter is the
// correct rendering, not a degraded one. Git prefers a long-running process
// filter over clean when one is configured, so both command forms are emptied;
// required is forced off so a disabled required filter does not fail the diff.
func filterNeutralizingConfig(dir string, args []string) []string {
	sub, cDir := gitSubcommand(args)
	if sub != "diff" {
		return nil
	}
	if cDir != "" {
		dir = cDir
	}
	drivers := localFilterDrivers(dir)
	if len(drivers) == 0 {
		return nil
	}
	out := make([]string, 0, 3*len(drivers))
	for _, name := range drivers {
		out = append(out,
			"filter."+name+".clean=",
			"filter."+name+".process=",
			"filter."+name+".required=false",
		)
	}
	return out
}

// gitSubcommand finds the first non-global argument — the subcommand — and the
// directory named by a leading -C, if the caller put it inside args instead of
// the dir parameter. Global options before the subcommand are limited to the
// forms gitcmd's call sites use (-c v, -C d, --opt=v, --opt d); anything else
// still terminates the scan at the first bare word.
func gitSubcommand(args []string) (sub, cDir string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C":
			if i+1 < len(args) {
				cDir = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-C"):
			cDir = strings.TrimPrefix(a, "-C")
		case a == "-c":
			i++ // skip the key=value that follows
		case strings.HasPrefix(a, "-"):
			// Any other global flag; --flag=value and bare flags alike carry
			// no value we track. The next bare word ends global options.
		default:
			return a, cDir
		}
	}
	return "", cDir
}

// localFilterDrivers lists the filter driver names defined by sections of the
// repository-local git config under dir. Only [filter "<name>"] sections are
// collected: the driver's command lives in the config, and the config is the
// part of a distributed repository its author controls. A missing or unreadable
// config yields no drivers (nothing to neutralize). User and system config are
// deliberately not read — those are the user's own choices.
func localFilterDrivers(dir string) []string {
	var drivers []string
	for _, cfgPath := range localGitConfigPaths(dir) {
		f, err := os.Open(cfgPath)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if len(line) < 2 || line[0] != '[' || line[len(line)-1] != ']' {
				continue
			}
			section := strings.TrimSpace(line[1 : len(line)-1])
			i := strings.IndexAny(section, " \t")
			if i < 0 || !strings.EqualFold(section[:i], "filter") {
				continue
			}
			name := strings.Trim(strings.TrimSpace(section[i:]), `"`)
			if name == "" || slices.Contains(drivers, name) {
				continue
			}
			drivers = append(drivers, name)
		}
		_ = f.Close()
	}
	return drivers
}

// localGitConfigPaths resolves the repository-local configs for a working tree.
// Linked worktrees inherit <commondir>/config and may add
// <gitdir>/config.worktree when extensions.worktreeConfig is enabled.
func localGitConfigPaths(dir string) []string {
	if dir == "" {
		dir = "."
	}
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return nil
	}
	gitdir := dotGit
	if !info.IsDir() {
		data, readErr := os.ReadFile(dotGit)
		if readErr != nil {
			return nil
		}
		line := strings.TrimSpace(string(data))
		rest, ok := strings.CutPrefix(line, "gitdir:")
		if !ok {
			return nil
		}
		gitdir = strings.TrimSpace(rest)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(dir, gitdir)
		}
	}
	gitdir = filepath.Clean(gitdir)
	commonDir := gitdir
	if data, readErr := os.ReadFile(filepath.Join(gitdir, "commondir")); readErr == nil {
		commonDir = strings.TrimSpace(string(data))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(gitdir, commonDir)
		}
		commonDir = filepath.Clean(commonDir)
	}
	paths := []string{filepath.Join(commonDir, "config")}
	worktreeConfig := filepath.Join(gitdir, "config.worktree")
	if worktreeConfig != paths[0] {
		paths = append(paths, worktreeConfig)
	}
	return paths
}

// hardenSubcommand adds the flags that disable repository-configured programs
// for the subcommands that can invoke them. The flags go after the subcommand
// name, where git accepts them, and are only added when absent so an explicit
// caller flag is never duplicated.
func hardenSubcommand(args []string) []string {
	if len(args) == 0 || args[0] != "diff" {
		return args
	}
	out := []string{args[0]}
	for _, flag := range []string{"--no-ext-diff", "--no-textconv"} {
		if !slices.Contains(args, flag) {
			out = append(out, flag)
		}
	}
	return append(out, args[1:]...)
}

// Command builds a hardened git command rooted at dir (empty runs in the
// process working directory). The environment drops credential variables so a
// git subprocess — and anything git itself starts — never inherits provider
// keys, and disables interactive prompts so a probe cannot block on one.
func Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return CommandWithConfig(ctx, dir, nil, args...)
}

// CommandWithConfig is Command with additional "key=value" config overrides
// layered on top of the baseline.
func CommandWithConfig(ctx context.Context, dir string, extraConfig []string, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := proc.CommandContext(ctx, "git", Args(dir, extraConfig, args...)...)
	cmd.Env = Env()
	proc.HideWindow(cmd)
	return cmd
}

// Env is the environment a git subprocess runs with. GIT_EXTERNAL_DIFF is
// covered by --no-ext-diff on diff invocations, which outranks both the config
// key and the environment variable. GIT_SSH_COMMAND is deliberately left
// alone: it governs the ssh network transport (fetch/ls-remote/push), not diff
// rendering, and clearing it would break legitimate ssh remotes; repository
// config that sets core.sshCommand is a plugin-trust concern, not one this
// diff-oriented baseline can address (see the package residual note).
func Env() []string {
	return append(secrets.ProcessEnv(),
		// Read-only probes must not take the index lock.
		"GIT_OPTIONAL_LOCKS=0",
		// Fail fast instead of blocking on a credential prompt for a terminal
		// the TUI owns and the desktop app does not have.
		"GIT_TERMINAL_PROMPT=0",
	)
}
