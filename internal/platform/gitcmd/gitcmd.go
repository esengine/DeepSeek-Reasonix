package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
)

// baseConfig is the -c override set every invocation carries.
var baseConfig = []string{
	// An index refresh executes this as a command when the repository sets
	// it. Empty rather than "false": git before 2.35.2 reads "false" as a
	// hook name.
	"core.fsmonitor=",
	// Keeps a probe from starting git's background maintenance daemon.
	"maintenance.auto=false",
	"gc.auto=0",
	// A hook path with no executables under it: checkout, merge and ref
	// updates run no hook.
	"core.hooksPath=" + os.DevNull,
	// Otherwise log and show run gpg.program on every signed commit.
	"log.showSignature=false",
	"merge.verifySignatures=false",
	// Signature formats (%G?, %(signature)) run these programs for any signed
	// object, whatever the repository names.
	"gpg.program=",
	"gpg.ssh.program=",
	"gpg.x509.program=",
	// No inline submodule diff starts a git process that reads the
	// submodule's own config. submodule.recurse is pinned per repository.
	"diff.submodule=short",
	// Discovery never lands on a bare repository by walking up: only a
	// repository named with GIT_DIR (see Repo) is opened as bare.
	"safe.bareRepository=explicit",
}

// Args returns the full argument list for a git invocation: the hardening
// overrides, an optional -C directory, then the caller's arguments. extraConfig
// entries are "key=value" pairs appended after the baseline, so a call site can
// add its own preferences but cannot drop the baseline. Args does not consult
// the repository; Command adds the per-repository driver overrides.
func Args(dir string, extraConfig []string, args ...string) []string {
	return argsFor(runtime.GOOS, dir, extraConfig, args...)
}

func argsFor(goos, dir string, extraConfig []string, args ...string) []string {
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
	if sub := subcommandIndex(args); sub >= 0 && slices.Contains(noRecurse, args[sub]) {
		out = append(out, "-c", "submodule.recurse=false")
	}
	if dir != "" {
		out = append(out, "-C", dirOperand(dir))
	}
	return append(out, hardenSubcommand(args)...)
}

// dirOperand spells a relative directory that starts with "-" as "./dir",
// the same directory, so no -C value can be read as an option.
func dirOperand(dir string) string {
	if dashed.MatchString(dir) {
		return "./" + dir
	}
	return dir
}

// noRecurse are the subcommands a user's own submodule.recurse=true would carry
// into each submodule, where that submodule's config applies. Host branch
// switches, merges and resets never update submodules.
var noRecurse = []string{"checkout", "switch", "restore", "reset", "merge", "read-tree"}

// globalsWithValue are git's global options that take their value as the next
// argument when not written with '='.
var globalsWithValue = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env", "--super-prefix", "--attr-source"}

// subcommandIndex returns the position of the subcommand in args, past any
// global options, or -1 when args names none.
func subcommandIndex(args []string) int {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return i
		}
		if slices.Contains(globalsWithValue, a) {
			i++
		}
	}
	return -1
}

// hardenSubcommand adds the flags that disable repository-configured programs
// for the subcommands that can invoke them. The flags go right after the
// subcommand, where git accepts them, and are only added when the caller has
// not already chosen that option.
func hardenSubcommand(args []string) []string {
	sub := subcommandIndex(args)
	if sub < 0 {
		return args
	}
	rest := args[sub+1:]
	var add []string
	switch args[sub] {
	case "diff", "log", "show":
		for _, flag := range []string{"--no-ext-diff", "--no-textconv"} {
			if !slices.Contains(rest, flag) {
				add = append(add, flag)
			}
		}
	}
	switch args[sub] {
	case "diff", "status":
		if !slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "--ignore-submodules") }) {
			add = append(add, "--ignore-submodules=dirty")
		}
	}
	if len(add) == 0 {
		return args
	}
	out := slices.Clone(args[:sub+1])
	out = append(out, add...)
	return append(out, rest...)
}

// Command builds a hardened git command rooted at dir (empty runs in the
// process working directory). The environment drops credential variables so a
// git subprocess — and anything git itself starts — never inherits provider
// keys, and disables interactive prompts so a probe cannot block on one.
func Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return CommandWithConfig(ctx, dir, nil, args...)
}

// CommandWithConfig is Command with additional "key=value" config overrides
// layered on top of the baseline. When the repository's drivers cannot be
// neutralized the command is returned unstartable: Run and Start report
// ErrRepositoryDrivers.
func CommandWithConfig(ctx context.Context, dir string, extraConfig []string, args ...string) *exec.Cmd {
	return build(ctx, dir, nil, extraConfig, args)
}

// build is every hardened invocation; repoEnv pins the repository (see Repo)
// for the driver listing and the command alike.
func build(ctx context.Context, dir string, repoEnv, extraConfig, args []string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	screened, refused := screen(args)
	overrides, err := driverOverrides(ctx, dir, repoEnv, screened)
	cmd := newCommand(ctx, Args(dir, append(slices.Clone(extraConfig), overrides...), screened...), repoEnv)
	if err == nil {
		err = refused
	}
	if err != nil {
		cmd.Err = err
	}
	return cmd
}

// Detached makes cmd run from a fresh empty directory above which git will not
// search, for commands that name their repository by URL: no repository's
// config reaches them. The returned cleanup removes the directory.
func Detached(cmd *exec.Cmd) (cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "reasonix-git-")
	if err != nil {
		return func() {}, err
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(strings.ToUpper(kv), "=")
		return name == "GIT_DIR" || name == "GIT_WORK_TREE" || name == "GIT_COMMON_DIR" || name == "GIT_CEILING_DIRECTORIES"
	})
	cmd.Dir = dir
	cmd.Env = append(env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	return func() { _ = os.RemoveAll(dir) }, nil
}

func newCommand(ctx context.Context, args, repoEnv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(Env(), repoEnv...)
	proc.HideWindow(cmd)
	return cmd
}

// availableProbeTimeout bounds the one probe below. A shim that resolves but
// cannot answer is the case being detected, and on macOS that shim may try to
// put up an installer dialog rather than exit.
const availableProbeTimeout = 3 * time.Second

// Available reports whether git can actually run, not whether something named
// git sits on PATH: on macOS /usr/bin/git is an Xcode stub that resolves with no
// command line tools installed and fails only when executed. Uncached on purpose
// — every caller is a low-frequency decision, and caching the "no" would make
// installing git afterwards require a restart to take effect.
func Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), availableProbeTimeout)
	defer cancel()
	return Command(ctx, "", "--version").Run() == nil
}

// Env is the environment a git subprocess runs with. GIT_EXTERNAL_DIFF and
// GIT_SSH_COMMAND are the user's own and stay: --no-ext-diff outranks the
// former, and the latter reaches only network commands, which run Detached.
func Env() []string {
	return append(secrets.ProcessEnv(),
		// Read-only probes must not take the index lock.
		"GIT_OPTIONAL_LOCKS=0",
		// Fail fast instead of blocking on a credential prompt for a terminal
		// the TUI owns and the desktop app does not have.
		"GIT_TERMINAL_PROMPT=0",
		// A missing object is an error, never a fetch through the
		// repository's remote configuration (git 2.44 and later).
		"GIT_NO_LAZY_FETCH=1",
	)
}
