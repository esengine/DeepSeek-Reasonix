package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"

	"reasonix/internal/i18n"
	"reasonix/internal/jobs"
	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// pwshCommandArgLimit is the wrapped-script length above which the command is
// passed as -EncodedCommand (UTF-16LE Base64) instead of -Command. Encoded
// form is immune to every quoting/escaping hazard and stays well under the
// Win32 32K command-line limit; 8000 leaves generous headroom for the argv
// prefix and any sandbox wrapper.
const pwshCommandArgLimit = 8000

var errPowerShellTimeout = errors.New("powershell foreground timeout")

// Registered so tool.LookupBuiltin("powershell") works and an explicit
// [tools].enabled = ["powershell", ...] doesn't warn "unknown built-in".
// Registration into per-run registries is gated on [tools.powershell]
// enabled = true (see Workspace.Tools and boot's addBuiltins), so the default
// tool list — and the cache-stable system-prompt prefix — is unchanged.
func init() { tool.RegisterBuiltin(powershell{}) }

var powershellSandboxCommandArgs = sandbox.CommandArgs

// powershell runs a command under a real PowerShell interpreter (pwsh 7+
// preferred, Windows PowerShell 5.1 as fallback). It is the Windows-oriented
// counterpart of bash: same plumbing (job manager, tracked spawn, sandbox
// approval flow, streaming progress), but argv is built for PowerShell
// semantics — process-scoped execution-policy bypass, UTF-8 console, real
// native exit codes, EncodedCommand for long scripts. shell is the resolved
// interpreter; the zero value resolves lazily via sandbox.ResolvePowerShell.
// sb, workDir, timeout and guard mirror the bash fields. There is no
// host-terminal handoff: the terminal runner executes whatever the host
// shell is, which for a tool named powershell would be a silent interpreter
// swap — foreground commands always run locally.
type powershell struct {
	sb      sandbox.Spec
	shell   sandbox.Shell
	guard   SessionDataGuard
	workDir string
	timeout time.Duration
}

type powershellParams struct {
	Command         string `json:"command"`
	RunInBackground bool   `json:"run_in_background"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

func (powershell) Name() string { return "powershell" }

func (p powershell) Description() string {
	sh := p.resolved()
	shellName := "PowerShell 7 (pwsh)"
	chaining := "'&&' and '||' are parsed for conditional chaining; ';' runs both regardless."
	if sh.Kind == sandbox.ShellPowerShell && sh.Path != "" && !sh.SupportsChaining() {
		shellName = "Windows PowerShell 5.1"
		chaining = "';' runs both regardless; 'if ($?) { ... }' is conditional. '&&' and '||' are NOT parsed."
	}
	return fmt.Sprintf("Run a PowerShell command under %s and return combined stdout/stderr. Prefer this over bash for PowerShell-native work (cmdlets, .NET APIs, Windows automation); use bash for POSIX pipelines."+
		" PowerShell quick reference:\n"+
		"  - chaining: %s\n"+
		"  - cmdlets are Verb-Noun; the pipeline passes .NET objects, not text — shape with Where-Object, Select-Object, ForEach-Object, Sort-Object, Measure-Object.\n"+
		"  - splat parameters with @{}: `$p = @{LiteralPath=$f; Destination=$d}; Copy-Item @p`. Parameter value expressions must be parenthesized: `-Index (100..120)`.\n"+
		"  - `foreach (...) { }` is a statement, not an expression — it cannot be piped; assign first or use ForEach-Object.\n"+
		"  - comparisons: -eq -ne -gt -ge -lt -le, -like (wildcard), -match (regex), -contains (collection). Logical: -and -or -not (or `!`).\n"+
		"  - strings: 'single' is literal; \"double\" expands $variables and $(subexpressions). Use ${name}_suffix for variable boundaries. Here-strings: @'...'@ (literal) / @\"...\"@ (expanded); the closing delimiter must be alone at line start.\n"+
		"  - environment: $env:NAME (session-scoped). Append PATH with `$env:PATH += ';new\\path'` — never overwrite. Child-process env changes do not propagate back.\n"+
		"  - native commands: `& $exe @argList`. $LASTEXITCODE holds the last native exit code; $? is whether the last command succeeded. Capture $LASTEXITCODE before piping.\n"+
		"  - file ops: Get-ChildItem (ls), Get-Content (cat), Remove-Item -Recurse -Force (rm -rf), Copy-Item (cp), Move-Item (mv), Select-String (grep). No head/tail/which/touch: Select-Object -First/-Last N, (Get-Command x).Source, New-Item.\n"+
		"  - paths: use backslashes (C:\\path\\to\\file); quote paths containing spaces. $null not /dev/null; '2>$null' drops stderr."+
		bashToolSteer, shellName, chaining)
}

func (powershell) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"PowerShell command to execute"},"run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns (no foreground timeout). Read new output with bash_output, wait with wait, stop it with kill_shell. Use for long-running commands like servers, watchers, or builds you don't need to block on."},"timeout_seconds":{"type":"integer","description":"Optional per-call foreground cap in seconds. Can only tighten the configured bash_timeout_seconds cap, never loosen it; omit for the default."}},"required":["command"]}`)
}

// ReadOnly is false for the same reason as bash: the effect of a PowerShell
// command cannot be inferred from its text.
func (powershell) ReadOnly() bool { return false }

func (powershell) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 40, Tail: 40, HeadChars: 8000, TailChars: 8000}
}

// resolved returns the bound interpreter, resolving lazily for the zero-value
// instance. The result is memoized process-wide by ResolvePowerShell.
func (p powershell) resolved() sandbox.Shell {
	if p.shell.Path != "" {
		return p.shell
	}
	if p.sb.Shell.Path != "" && p.sb.Shell.Kind == sandbox.ShellPowerShell {
		return p.sb.Shell
	}
	return sandbox.ResolvePowerShell("", "", nil)
}

func (p powershell) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var pp powershellParams
	if err := json.Unmarshal(args, &pp); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := validatePowerShellCommand(pp.Command); err != nil {
		return "", err
	}

	sh := p.resolved()
	if sh.Path == "" || sh.Kind != sandbox.ShellPowerShell {
		return "", fmt.Errorf("no PowerShell interpreter found on this host; install PowerShell 7 (https://aka.ms/powershell) or set [tools.powershell] path")
	}
	if !sh.SupportsChaining() && (hasUnquotedSeq(pp.Command, "&&") || hasUnquotedSeq(pp.Command, "||")) {
		return "", fmt.Errorf("this shell is Windows PowerShell 5.1, which does not parse '&&' or '||'. " +
			"Sequence with ';' (both run regardless of the first's result), use 'if ($?) { ... }' for " +
			"conditional chaining, or issue the commands as separate calls")
	}

	// wrap → encode → sandbox-wrap argv. The console init + try/catch wrapper
	// makes terminating errors non-zero and preserves real native exit codes;
	// pwshArgv switches to EncodedCommand past the length limit; the OS
	// sandbox wraps the finished argv (on Windows it returns argv unwrapped).
	wrapped := wrapPowerShellCommand(pp.Command)
	argv, sandboxed := powershellSandboxCommandArgs(p.sb, pwshArgv(sh, wrapped))
	if p.sb.Enforce() && bashSandboxEscapeSessionAllowed(ctx, pp.Command, args) {
		argv = pwshArgv(sh, wrapped)
		sandboxed = false
	} else if p.sb.Enforce() && !sandboxed {
		allow, reason, err := approveBashSandboxEscape(ctx, pp.Command, args, i18n.M.SandboxEscapeWrapReason)
		if err != nil {
			return "", err
		}
		if !allow {
			if reason != "" {
				return "", fmt.Errorf("%s", reason)
			}
			return "", fmt.Errorf("%s", sandbox.UnavailableMessage())
		}
		argv = pwshArgv(sh, wrapped)
	}
	cmdEnv := bashCommandEnv(ctx)

	if pp.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		workDir := p.workDir
		// The job runs under the manager's session context (no foreground timeout), so it
		// survives this turn; its combined output streams to the job buffer.
		job := jm.StartForSession(jobs.SessionFromContext(ctx), "powershell", commandPreview(pp.Command), func(jobCtx context.Context, out io.Writer) (string, error) {
			cmd := exec.CommandContext(jobCtx, argv[0], argv[1:]...)
			cmd.Dir = workDir
			cmd.Env = cmdEnv
			cmd.WaitDelay = bashWaitDelay
			cmd.Stdout = out
			cmd.Stderr = out
			tracked, runErr := runPowerShellProcess(jobCtx, cmd, sh, pp.Command, shouldTrackShellProcess(sandboxed, sh, pp.Command, false))
			if shouldReapAfterRun(jobCtx, sh, pp.Command, false) {
				reapShellProcess(cmd, tracked) // reap process-group stragglers the job left running (#3702)
			}
			return "", normalizeBashRunError(jobCtx, runErr, false)
		})
		msg := fmt.Sprintf("Started background job %q. It keeps running across turns; read new output with bash_output(job_id=%q), wait for it with wait, or stop it with kill_shell(job_id=%q).", job.ID, job.ID, job.ID)
		return appendSessionDataHint(msg, p.guard.CommandHint(p.workDir, pp.Command)), nil
	}

	out, err := p.runForeground(ctx, pp, sh, argv, sandboxed, cmdEnv)
	return appendSessionDataHint(out, p.guard.CommandHint(p.workDir, pp.Command)), err
}

func (p powershell) runForeground(ctx context.Context, pp powershellParams, sh sandbox.Shell, argv []string, sandboxed bool, cmdEnv []string) (string, error) {
	runCtx := ctx
	timeout := p.foregroundTimeout(pp.TimeoutSeconds)
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeoutCause(ctx, timeout, errPowerShellTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = p.workDir // "" lets exec use the process working directory
	cmd.Env = cmdEnv
	cmd.WaitDelay = bashWaitDelay
	var buf bytes.Buffer
	w := io.Writer(&buf)
	if emit, ok := tool.ProgressFrom(ctx); ok {
		w = io.MultiWriter(&buf, newProgressWriter(emit))
	}
	cmd.Stdout = w
	cmd.Stderr = w
	tracked, err := runPowerShellProcess(runCtx, cmd, sh, pp.Command, shouldTrackShellProcess(sandboxed, sh, pp.Command, false))
	if shouldReapAfterRun(runCtx, sh, pp.Command, false) {
		reapShellProcess(cmd, tracked)
	}
	err = normalizeBashRunError(runCtx, err, false)
	out := buf.String()

	if errors.Is(context.Cause(runCtx), errPowerShellTimeout) {
		return out, fmt.Errorf("command timed out (> %s)", timeout)
	}
	if err != nil {
		// Non-zero exit: feed output and error back so the model can self-correct.
		return out, fmt.Errorf("command exited: %w", err)
	}
	return out, nil
}

// foregroundTimeout caps a foreground call. The per-call timeout_seconds can
// only tighten the configured cap, never loosen it.
func (p powershell) foregroundTimeout(perCallSeconds int) time.Duration {
	timeout := p.timeout
	if perCallSeconds > 0 {
		perCall := time.Duration(perCallSeconds) * time.Second
		if timeout <= 0 || perCall < timeout {
			timeout = perCall
		}
	}
	return timeout
}

func runPowerShellProcess(ctx context.Context, cmd *exec.Cmd, sh sandbox.Shell, command string, track bool) (*proc.TrackedCommand, error) {
	return proc.RunCommand(ctx, cmd, proc.RunOptions{
		Track:           track,
		CancelWaitGrace: bashWaitDelay + time.Second,
		Source:          "powershell_tool",
		ShellKind:       sh.Kind.String(),
		ShellPath:       sh.Path,
		CommandPreview:  commandPreview(command),
	})
}

// wrapPowerShellCommand prepares cmd for captured execution: the UTF-8 console
// prologue (shared with the bash PowerShell fallback), Ctrl-C passthrough, and
// a try/catch that turns terminating errors into exit 1 while letting real
// native exit codes survive — a bare -Command flattens every failure to 1.
func wrapPowerShellCommand(cmd string) string {
	return sandbox.PowerShellUTF8Script(
		"try{[Console]::TreatControlCAsInput=$true}catch{};" +
			"try{" + cmd + "}catch{$_|Out-String|Write-Error;exit 1};exit $LASTEXITCODE")
}

// pwshArgv builds the invocation argv: stable flags first (process-scoped
// execution-policy bypass — it never mutates system policy), then exactly one
// payload argument, always last. Scripts longer than pwshCommandArgLimit go as
// -EncodedCommand (UTF-16LE Base64), which is immune to all quoting issues.
func pwshArgv(sh sandbox.Shell, wrapped string) []string {
	path := sh.Path
	if path == "" {
		path = sh.Kind.String()
	}
	argv := []string{path, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-NoLogo"}
	if len(wrapped) > pwshCommandArgLimit {
		return append(argv, "-EncodedCommand", encodePowerShellCommand(wrapped))
	}
	return append(argv, "-Command", wrapped)
}

// encodePowerShellCommand renders the -EncodedCommand payload: UTF-16LE bytes,
// Base64. (PowerShell's -EncodedCommand contract is UCS-2/UTF-16LE.)
func encodePowerShellCommand(s string) string {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// validatePowerShellCommand fail-fast checks the command text before any
// process is spawned: empty commands are rejected, and a double quote left
// open at end of input almost always means the model mis-escaped something —
// better to return an actionable error than a cryptic parser message. The
// quote check is a heuristic: it tracks single-quoted spans (where " is
// literal), backtick escapes, and PowerShell's doubled-quote escapes; exotic
// constructs can still slip past either way, so it only rejects the certain
// case.
func validatePowerShellCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("command is required")
	}
	var quote byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			if c == '`' && quote == '"' && i+1 < len(cmd) {
				i++ // backtick escape inside a double-quoted string
				continue
			}
			if c == quote {
				if i+1 < len(cmd) && cmd[i+1] == quote {
					i++ // doubled-quote escape ('' inside '', "" inside "")
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		}
	}
	if quote == '"' {
		return fmt.Errorf("command has unbalanced double quotes — check quoting/escaping (use 'single quotes' for literal text, backtick-escape `\" inside \"...\", or a here-string)")
	}
	return nil
}
