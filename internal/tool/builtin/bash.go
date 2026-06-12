package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/jobs"
	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

const (
	bashWaitDelay = 5 * time.Second
)

var errBashTimeout = errors.New("bash foreground timeout")

func init() { tool.RegisterBuiltin(bash{}) }

var bashShellPATH = cachedBashShellPATH

// cachedBashShellPATH memoizes the login-shell PATH probe per login shell so a
// shell isn't spawned on every bash tool call (the probe runs up to three
// interactive-login shells with a 2s timeout each). Empty results are cached too,
// so a host without a usable login shell doesn't re-probe each command.
var (
	bashPathMu    sync.Mutex
	bashPathCache = map[string]string{}
)

func cachedBashShellPATH(ctx context.Context) string {
	key := loginShell()
	bashPathMu.Lock()
	if v, ok := bashPathCache[key]; ok {
		bashPathMu.Unlock()
		return v
	}
	bashPathMu.Unlock()

	v := defaultBashShellPATH(ctx)

	bashPathMu.Lock()
	bashPathCache[key] = v
	bashPathMu.Unlock()
	return v
}

// bash runs a shell command. sb, when it enforces, wraps the command in an OS
// sandbox; the zero value registered at init runs unconfined and is overridden
// per run by ConfineBash. shell is the resolved interpreter (real bash, or
// PowerShell on a Windows host without bash); the zero value resolves lazily.
// workDir, when non-empty, is the directory the command runs in (cmd.Dir);
// empty uses the process cwd. timeout optionally caps foreground commands;
// zero or negative means no tool-local cap, while parent context cancellation
// still kills the process tree.
type bash struct {
	sb      sandbox.Spec
	shell   sandbox.Shell
	workDir string
	timeout time.Duration
}

func (bash) Name() string { return "bash" }

func (b bash) Description() string {
	if b.resolved().Kind == sandbox.ShellPowerShell {
		return "Execute a command in the shell and return combined stdout/stderr. " +
			"NOTE: bash is not available on this host — commands run under Windows PowerShell, so write PowerShell, not bash:\n" +
			"  - chaining: ';' runs both regardless; 'if ($?) { ... }' is conditional. '&&' and '||' are NOT parsed.\n" +
			"  - redirect/vars: $null not /dev/null; $env:VAR not $VAR; '2>$null' drops stderr.\n" +
			"  - file ops: Get-ChildItem (ls), Get-Content (cat), Remove-Item -Recurse -Force (rm -rf), Copy-Item (cp), Select-String (grep).\n" +
			"  - no head/tail/which/touch: use Select-Object -First/-Last N, (Get-Command x).Source, New-Item.\n" +
			"  - multi-line text to a native exe (e.g. git commit -m): use a single-quoted here-string @'...'@ (closing '@ at column 0)." +
			bashToolSteer
	}
	return "Execute a command in the shell and return combined stdout/stderr." + bashToolSteer
}

// bashToolSteer points the model at the cross-platform built-in tools instead of
// shell utilities, so it doesn't reach for grep/cat/ls/find (absent or different
// on native Windows) when a native tool already does the job everywhere.
const bashToolSteer = " Use for builds, tests, git, package managers, etc. To search/read/list/edit files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file) over shell grep/cat/ls/find/sed — they behave identically on every OS. For symbol search, call graphs, or architecture questions, use codegraph tools instead of grep."

// resolved returns the bound shell, resolving lazily for the zero-value instance
// (e.g. a registry that never went through ConfineBash).
func (b bash) resolved() sandbox.Shell {
	if b.shell.Path != "" {
		return b.shell
	}
	if b.sb.Shell.Path != "" {
		return b.sb.Shell
	}
	return sandbox.ResolveShell("", "", nil)
}

func (bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns (no foreground timeout). Read new output with bash_output, wait for it with wait, stop it with kill_shell. Use for long-running commands like servers, watchers, or builds you don't need to block on."}},"required":["command"]}`)
}

// ReadOnly is false: bash's effect cannot be inferred from args (rm, curl,
// git commit, etc. are all reachable). Conservative even when a particular
// command happens to be read-only — the agent batch decision can't tell.
func (bash) ReadOnly() bool { return false }

func (b bash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	sh := b.resolved()
	if !sh.SupportsChaining() && (hasUnquotedSeq(p.Command, "&&") || hasUnquotedSeq(p.Command, "||")) {
		return "", fmt.Errorf("this shell is Windows PowerShell, which does not parse '&&' or '||'. " +
			"Sequence with ';' (both run regardless of the first's result), use 'if ($?) { ... }' for " +
			"conditional chaining, or issue the commands as separate calls")
	}

	// Wrap in the OS sandbox when configured; otherwise argv is just the shell.
	argv, _ := sandbox.Command(b.sb, sh, p.Command)
	cmdEnv := bashCommandEnv(ctx)

	if p.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		workDir := b.workDir
		// The job runs under the manager's session context (no foreground timeout), so it
		// survives this turn; its combined output streams to the job buffer.
		job := jm.Start("bash", commandPreview(p.Command), func(jobCtx context.Context, out io.Writer) (string, error) {
			cmd := exec.CommandContext(jobCtx, argv[0], argv[1:]...)
			cmd.Dir = workDir
			cmd.Env = cmdEnv
			setKillTree(cmd)
			cmd.WaitDelay = bashWaitDelay
			cmd.Stdout = out
			cmd.Stderr = out
			runErr := cmd.Run()
			reapTree(cmd) // reap process-group stragglers the job left running (#3702)
			return "", runErr
		})
		return fmt.Sprintf("Started background job %q. It keeps running across turns; read new output with bash_output(job_id=%q), wait for it with wait, or stop it with kill_shell(job_id=%q).", job.ID, job.ID, job.ID), nil
	}

	runCtx := ctx
	timeout := b.foregroundTimeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeoutCause(ctx, timeout, errBashTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = b.workDir // "" lets exec use the process working directory
	cmd.Env = cmdEnv
	setKillTree(cmd)
	cmd.WaitDelay = bashWaitDelay
	var buf bytes.Buffer
	w := io.Writer(&buf)
	if emit, ok := tool.ProgressFrom(ctx); ok {
		w = io.MultiWriter(&buf, newProgressWriter(emit))
	}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	// A foreground command that spawned a lingering child (e.g. `bazel run`'s
	// server) leaves it in the process group; Wait only reaped the shell leader.
	// Kill the group so those don't accumulate into an OOM (#3702). On cancel/
	// timeout setKillTree's Cancel already did this; this covers normal exit.
	reapTree(cmd)
	out := buf.String()

	if errors.Is(context.Cause(runCtx), errBashTimeout) {
		return out, fmt.Errorf("command timed out (> %s)", timeout)
	}
	if err != nil {
		// Non-zero exit: feed output and error back so the model can self-correct.
		return out, fmt.Errorf("command exited: %w", err)
	}
	return out, nil
}

func (b bash) foregroundTimeout() time.Duration {
	if b.timeout <= 0 {
		return 0
	}
	return b.timeout
}

// progressWriter forwards each chunk the command writes to a tool.ProgressFunc,
// so foreground bash output streams to the frontend as it is produced.
type progressWriter struct{ emit tool.ProgressFunc }

func newProgressWriter(emit tool.ProgressFunc) *progressWriter { return &progressWriter{emit: emit} }

func (w *progressWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}

// hasUnquotedSeq reports whether seq appears in s outside any single- or
// double-quoted span, so a literal "a && b" string argument doesn't trip the
// PowerShell chaining guard.
func hasUnquotedSeq(s, seq string) bool {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if strings.HasPrefix(s[i:], seq) {
			return true
		}
	}
	return false
}

// commandPreview is a short single-line label for a background bash job, surfaced
// in the status bar and completion notices.
func commandPreview(cmd string) string {
	cmd = strings.TrimSpace(strings.ReplaceAll(cmd, "\n", " "))
	const max = 48
	r := []rune(cmd)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return cmd
}

func bashCommandEnv(ctx context.Context) []string {
	env := os.Environ()
	if runtime.GOOS == "windows" {
		return env
	}
	currentPath, _ := envValue(env, "PATH")
	if shellPath := strings.TrimSpace(bashShellPATH(ctx)); shellPath != "" {
		if merged := mergePathLists(shellPath, currentPath); merged != currentPath {
			env = setEnvValue(env, "PATH", merged)
		}
	}
	return env
}

func defaultBashShellPATH(ctx context.Context) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := loginShell()
	if shell == "" {
		return ""
	}
	const marker = "__REASONIX_BASH_PATH__="
	script := "printf '\\n" + marker + "%s\\n' \"$PATH\""
	for _, args := range [][]string{
		{"-l", "-i", "-c", script},
		{"-l", "-c", script},
		{"-c", script},
	} {
		out := runShellPATHCommand(ctx, shell, args)
		if path := parseShellPATH(out, marker); path != "" {
			return path
		}
	}
	return ""
}

func loginShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		if hasPathSeparator(shell) {
			if isExecutableFile(shell) {
				return shell
			}
		} else if p, err := exec.LookPath(shell); err == nil {
			return p
		}
	}
	for _, shell := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if isExecutableFile(shell) {
			return shell
		}
	}
	return ""
}

func runShellPATHCommand(parent context.Context, shell string, args []string) []byte {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, args...)
	proc.PrepareShellPATHProbe(cmd)
	cmd.Stdin = strings.NewReader("")
	out, _ := cmd.CombinedOutput()
	return out
}

func parseShellPATH(out []byte, marker string) string {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], marker) {
			return strings.TrimSpace(strings.TrimPrefix(lines[i], marker))
		}
	}
	return ""
}

func hasPathSeparator(s string) bool {
	return strings.ContainsAny(s, `/\`)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func setEnvValue(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok && envKeyEqual(k, key) {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}
	return out
}

func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && envKeyEqual(k, key) {
			return v, true
		}
	}
	return "", false
}

func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func mergePathLists(primary, secondary string) string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		for _, part := range filepath.SplitList(path) {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	add(primary)
	add(secondary)
	return strings.Join(out, string(os.PathListSeparator))
}
