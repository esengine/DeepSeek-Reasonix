package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/jobs"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

const bashTimeout = 120 * time.Second

func init() { tool.RegisterBuiltin(bash{}) }

// bash runs a shell command with a timeout to avoid hangs. sb, when it enforces,
// wraps the command in an OS sandbox; the zero value registered at init runs
// unconfined and is overridden per run by ConfineBash. shell is the resolved
// interpreter (real bash, or PowerShell on a Windows host without bash); the
// zero value resolves lazily. workDir, when non-empty, is the directory the
// command runs in (cmd.Dir); empty uses the process cwd.
type bash struct {
	sb      sandbox.Spec
	shell   sandbox.Shell
	workDir string
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
const bashToolSteer = " Use for builds, tests, git, package managers, etc. To search/read/list/edit files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file) over shell grep/cat/ls/find/sed — they behave identically on every OS."

// resolved returns the bound shell, resolving lazily for the zero-value instance
// (e.g. a registry that never went through ConfineBash).
func (b bash) resolved() sandbox.Shell {
	if b.shell.Path != "" {
		return b.shell
	}
	return sandbox.ResolveShell()
}

func (bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns (no timeout). Read new output with bash_output, wait for it with wait, stop it with kill_shell. Use for long-running commands like servers, watchers, or builds you don't need to block on."}},"required":["command"]}`)
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

	if p.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		workDir := b.workDir
		// The job runs under the manager's session context (no 120s timeout), so it
		// survives this turn; its combined output streams to the job buffer.
		job := jm.Start("bash", commandPreview(p.Command), func(jobCtx context.Context, out io.Writer) (string, error) {
			cmd := exec.CommandContext(jobCtx, argv[0], argv[1:]...)
			cmd.Dir = workDir
			cmd.Stdout = out
			cmd.Stderr = out
			return "", cmd.Run()
		})
		return fmt.Sprintf("Started background job %q. It keeps running across turns; read new output with bash_output(job_id=%q), wait for it with wait, or stop it with kill_shell(job_id=%q).", job.ID, job.ID, job.ID), nil
	}

	ctx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = b.workDir // "" lets exec use the process working directory
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()

	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("command timed out (> %s)", bashTimeout)
	}
	if err != nil {
		// Non-zero exit: feed output and error back so the model can self-correct.
		return out, fmt.Errorf("command exited: %w", err)
	}
	return out, nil
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
