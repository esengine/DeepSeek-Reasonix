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
// Known residual: content filters (filter.<driver>.clean/smudge) and textconv
// drivers are selected per driver name through .gitattributes, so they cannot
// be disabled by a fixed key list the way the settings above can. Diff-side
// drivers are covered by --no-ext-diff/--no-textconv; checkout/status-side
// clean filters are not, and would need a different mechanism.
package gitcmd

import (
	"context"
	"os/exec"
	"runtime"
	"slices"

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
	if dir != "" {
		out = append(out, "-C", dir)
	}
	return append(out, hardenSubcommand(args)...)
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

// Env is the environment a git subprocess runs with. GIT_EXTERNAL_DIFF and
// GIT_SSH_COMMAND are deliberately left alone: an empty value is a present
// value to git, so clearing them would break legitimate ssh remotes rather than
// harden anything, and --no-ext-diff already outranks both the config key and
// the environment variable.
func Env() []string {
	return append(secrets.ProcessEnv(),
		// Read-only probes must not take the index lock.
		"GIT_OPTIONAL_LOCKS=0",
		// Fail fast instead of blocking on a credential prompt for a terminal
		// the TUI owns and the desktop app does not have.
		"GIT_TERMINAL_PROMPT=0",
	)
}
