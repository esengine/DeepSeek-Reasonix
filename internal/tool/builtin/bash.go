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
	sh := b.resolved()
	if sh.Kind == sandbox.ShellPowerShell {
		shellName := "Windows PowerShell"
		chaining := "';' runs both regardless; 'if ($?) { ... }' is conditional. '&&' and '||' are NOT parsed."
		if sh.SupportsChaining() {
			shellName = "PowerShell 7 (pwsh)"
			chaining = "'&&' and '||' are parsed for conditional chaining; ';' runs both regardless."
		}
		return fmt.Sprintf("Execute a command in the shell and return combined stdout/stderr. "+
			"NOTE: bash is not available on this host — commands run under %s, so write PowerShell, not bash:\n"+
			"  - chaining: %s\n"+
			"  - redirect/vars: $null not /dev/null; $env:VAR not $VAR; '2>$null' drops stderr.\n"+
			"  - file ops: Get-ChildItem (ls), Get-Content (cat), Remove-Item -Recurse -Force (rm -rf), Copy-Item (cp), Select-String (grep).\n"+
			"  - no head/tail/which/touch: use Select-Object -First/-Last N, (Get-Command x).Source, New-Item.\n"+
			"  - multi-line text to a native exe (e.g. git commit -m): use a single-quoted here-string @'...'@ (closing '@ at column 0)."+
			bashToolSteer, shellName, chaining)
	}
	return "Execute a command in the shell and return combined stdout/stderr." + bashToolSteer
}

// bashToolSteer points the model at the cross-platform built-in tools instead of
// shell utilities, so it doesn't reach for grep/cat/ls/find (absent or different
// on native Windows) when a native tool already does the job everywhere.
const bashToolSteer = " Use for builds, tests, git, package managers, etc. To search/read/list/edit/move files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file, move_file) over shell grep/cat/ls/find/sed/mv/Move-Item — they behave identically on every OS. For symbol search or architecture questions, prefer LSP/read tools and targeted grep before shell commands."

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
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns (no foreground timeout). Read new output with bash_output, wait with wait, stop it with kill_shell. Use for long-running commands like servers, watchers, or builds you don't need to block on."},"preserve_background_processes":{"type":"boolean","description":"After the shell command exits normally, keep any process-group members it intentionally left behind. Use only for deliberate daemonization, such as nohup/disown/setsid; cancellation and timeouts still kill the process group."}},"required":["command"]}`)
}

// ReadOnly is false: bash's effect cannot be inferred from args (rm, curl,
// git commit, etc. are all reachable). Conservative even when a particular
// command happens to be read-only — the agent batch decision can't tell.
func (bash) ReadOnly() bool { return false }

func (b bash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command                     string `json:"command"`
		RunInBackground             bool   `json:"run_in_background"`
		PreserveBackgroundProcesses bool   `json:"preserve_background_processes"`
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
	argv, sandboxed := sandbox.Command(b.sb, sh, p.Command)

	// When OS sandbox is unavailable but enforce is requested, apply in-process
	// write-path validation so bash cannot bypass the file-tool sandbox (#5793).
	if !sandboxed && b.sb.Enforce() {
		if err := checkBashWriteTargets(p.Command, sh, b.sb.WriteRoots); err != nil {
			return "", err
		}
	}
	cmdEnv := bashCommandEnv(ctx)

	if p.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		workDir := b.workDir
		// The job runs under the manager's session context (no foreground timeout), so it
		// survives this turn; its combined output streams to the job buffer.
		job := jm.StartForSession(jobs.SessionFromContext(ctx), "bash", commandPreview(p.Command), func(jobCtx context.Context, out io.Writer) (string, error) {
			cmd := exec.CommandContext(jobCtx, argv[0], argv[1:]...)
			cmd.Dir = workDir
			cmd.Env = cmdEnv
			cmd.WaitDelay = bashWaitDelay
			cmd.Stdout = out
			cmd.Stderr = out
			tracked, runErr := runShellProcess(cmd, shouldTrackShellProcess(sh, p.Command, p.PreserveBackgroundProcesses))
			if shouldReapAfterRun(jobCtx, sh, p.Command, p.PreserveBackgroundProcesses) {
				reapShellProcess(cmd, tracked) // reap process-group stragglers the job left running (#3702)
			}
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
	cmd.WaitDelay = bashWaitDelay
	var buf bytes.Buffer
	w := io.Writer(&buf)
	if emit, ok := tool.ProgressFrom(ctx); ok {
		w = io.MultiWriter(&buf, newProgressWriter(emit))
	}
	cmd.Stdout = w
	cmd.Stderr = w
	tracked, err := runShellProcess(cmd, shouldTrackShellProcess(sh, p.Command, p.PreserveBackgroundProcesses))
	// A foreground command that spawned a lingering child (e.g. `bazel run`'s
	// server) leaves it in the process group; Wait only reaped the shell leader.
	// Kill the group so those don't accumulate into an OOM (#3702). On cancel/
	// timeout the command's Cancel path already did this; this covers normal exit.
	if shouldReapAfterRun(runCtx, sh, p.Command, p.PreserveBackgroundProcesses) {
		reapShellProcess(cmd, tracked)
	}
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

func shouldReapAfterRun(ctx context.Context, sh sandbox.Shell, command string, preserveBackgroundProcesses bool) bool {
	if ctx.Err() != nil {
		return true
	}
	if preserveBackgroundProcesses {
		return false
	}
	return sh.Kind != sandbox.ShellBash || !hasExplicitBackgroundKeepalive(command)
}

// hasExplicitBackgroundKeepalive detects common shell-level daemonization intent
// without letting a plain "cmd &" bypass #3702's stray process cleanup.
func hasExplicitBackgroundKeepalive(command string) bool {
	if !hasUnquotedBackgroundOperator(command) {
		return false
	}
	return hasShellCommandWord(command, map[string]struct{}{
		"disown": {},
		"nohup":  {},
		"setsid": {},
	})
}

func (b bash) foregroundTimeout() time.Duration {
	if b.timeout <= 0 {
		return 0
	}
	return b.timeout
}

type trackedShellProcess struct {
	cmd    *exec.Cmd
	mu     sync.Mutex
	job    uintptr
	killed bool
}

func shouldTrackShellProcess(sh sandbox.Shell, command string, preserveBackgroundProcesses bool) bool {
	if preserveBackgroundProcesses {
		return false
	}
	return sh.Kind != sandbox.ShellBash || !hasExplicitBackgroundKeepalive(command)
}

func runShellProcess(cmd *exec.Cmd, track bool) (*trackedShellProcess, error) {
	if !track {
		setKillTree(cmd)
		return nil, cmd.Run()
	}
	tracked := &trackedShellProcess{cmd: cmd}
	proc.HideWindow(cmd)
	cmd.Cancel = func() error {
		tracked.kill()
		return context.Canceled
	}
	job, err := proc.StartTracked(cmd)
	if err != nil {
		return tracked, err
	}
	tracked.setJob(job)
	return tracked, cmd.Wait()
}

func reapShellProcess(cmd *exec.Cmd, tracked *trackedShellProcess) {
	if tracked != nil {
		tracked.kill()
		return
	}
	reapTree(cmd)
}

func (p *trackedShellProcess) setJob(job uintptr) {
	if p == nil || job == 0 {
		return
	}
	p.mu.Lock()
	killed := p.killed
	if !killed {
		p.job = job
	}
	p.mu.Unlock()
	if killed {
		proc.KillTracked(p.cmd, job)
	}
}

func (p *trackedShellProcess) kill() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.killed {
		p.mu.Unlock()
		return
	}
	p.killed = true
	job := p.job
	p.job = 0
	p.mu.Unlock()
	proc.KillTracked(p.cmd, job)
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

func hasUnquotedBackgroundOperator(s string) bool {
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
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
		if c != '&' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '&' {
			i++
			continue
		}
		prev := previousNonSpace(s, i)
		if prev == '>' {
			continue
		}
		next := nextNonSpace(s, i+1)
		if next == '>' {
			continue
		}
		return true
	}
	return false
}

func hasShellCommandWord(s string, want map[string]struct{}) bool {
	expectCommand := true
	skipNextWord := false
	for i := 0; i < len(s); {
		c := s[i]
		if isShellSpace(c) {
			i++
			continue
		}
		switch c {
		case ';', '\n', '&', '|', '(':
			if i+1 < len(s) && (s[i:i+2] == "&&" || s[i:i+2] == "||") {
				i += 2
			} else {
				i++
			}
			expectCommand = true
			skipNextWord = false
			continue
		case '<', '>':
			i = skipShellRedirect(s, i)
			skipNextWord = true
			continue
		}

		word, next := readShellWord(s, i)
		i = next
		if word == "" {
			continue
		}
		if skipNextWord {
			skipNextWord = false
			continue
		}
		if !expectCommand {
			continue
		}
		if isShellAssignment(word) {
			continue
		}
		base := shellWordBase(word)
		if _, ok := want[base]; ok {
			return true
		}
		if base == "command" || base == "env" {
			continue
		}
		expectCommand = false
	}
	return false
}

func readShellWord(s string, start int) (string, int) {
	var b strings.Builder
	for i := start; i < len(s); i++ {
		c := s[i]
		if isShellSpace(c) || strings.ContainsRune(";|&()<>", rune(c)) {
			return b.String(), i
		}
		switch c {
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		case '\'':
			for i++; i < len(s) && s[i] != '\''; i++ {
				b.WriteByte(s[i])
			}
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), len(s)
}

func skipShellRedirect(s string, i int) int {
	for i < len(s) && (s[i] == '<' || s[i] == '>' || s[i] == '&') {
		i++
	}
	return i
}

func previousNonSpace(s string, before int) byte {
	for i := before - 1; i >= 0; i-- {
		if !isShellSpace(s[i]) {
			return s[i]
		}
	}
	return 0
}

func nextNonSpace(s string, after int) byte {
	for i := after; i < len(s); i++ {
		if !isShellSpace(s[i]) {
			return s[i]
		}
	}
	return 0
}

func isShellSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func isShellAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return false
			}
			continue
		}
		if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func shellWordBase(word string) string {
	if i := strings.LastIndexByte(word, '/'); i >= 0 {
		return word[i+1:]
	}
	return word
}

// checkBashWriteTargets scans command for file-write patterns (redirects,
// PowerShell cmdlets, tee, dd) and validates every detected output path against
// writeRoots. When OS sandbox is unavailable, this in-process check prevents the
// model from using bash to bypass the file-tool sandbox (#5793).
//
// It is a best-effort defence: it catches the common patterns the model learns
// (Set-Content, Out-File, >/>> redirects) but cannot intercept writes made by a
// sub-program the shell invokes (python -c "open(...)"). The permission layer
// already gates the bash call itself; this guard closes the "approve once,
// write anywhere" gap on platforms without OS sandboxing.
func checkBashWriteTargets(command string, sh sandbox.Shell, writeRoots []string) error {
	if len(writeRoots) == 0 {
		return nil
	}
	roots := realRoots(writeRoots)
	for _, target := range extractWriteTargets(command, sh) {
		if err := confine(roots, target); err != nil {
			return fmt.Errorf("bash sandbox: %w. "+
				"Use the dedicated write tools (write_file, edit_file) to modify files — "+
				"they respect the sandbox. To bypass, set [sandbox] bash = \"off\" in reasonix.toml.",
				err)
		}
	}
	return nil
}

// extractWriteTargets returns file paths that the command appears to write to.
// It handles common patterns the model uses to bypass the file-tool sandbox:
// PowerShell Set-Content/Out-File/Add-Content -Path, shell >/>> redirects,
// tee, and dd of=. It does not parse arbitrary subcommands.
func extractWriteTargets(command string, sh sandbox.Shell) []string {
	var targets []string

	if sh.Kind == sandbox.ShellPowerShell {
		targets = append(targets, extractPSWriteTargets(command)...)
	}

	// Shell redirect targets: >file, >>file, 2>file, &>file, 1>file
	targets = append(targets, extractRedirectTargets(command)...)

	// tee file1 file2 ...
	targets = append(targets, extractTeeTargets(command)...)

	// dd of=file
	targets = append(targets, extractDdTargets(command)...)

	return targets
}

// extractPSWriteTargets extracts -Path and -FilePath arguments from PowerShell
// write cmdlets (Set-Content, Out-File, Add-Content, New-Item,
// Export-Csv, Export-Clixml).
func extractPSWriteTargets(command string) []string {
	writeCmdlets := []string{
		"Set-Content", "Out-File", "Add-Content", "New-Item",
		"Export-Csv", "Export-Clixml",
	}

	var targets []string
	// Scan for each write cmdlet and its -Path / -FilePath argument.
	for _, c := range writeCmdlets {
		rest := command
		for {
			idx := findCmdlet(rest, c)
			if idx < 0 {
				break
			}
			// Search for -Path or -FilePath after the cmdlet name.
			tail := rest[idx+len(c):]
			path := extractPSPathArg(tail)
			if path != "" {
				targets = append(targets, path)
			}
			rest = rest[idx+len(c):]
		}
	}
	return targets
}

// findCmdlet finds the next occurrence of cmdlet in command, returning the
// start index or -1. It matches only when cmdlet appears as a whole word
// (preceded by start-of-string, whitespace, or a pipe/command separator).
func findCmdlet(command, cmdlet string) int {
	for i := 0; i <= len(command)-len(cmdlet); i++ {
		sub := command[i : i+len(cmdlet)]
		if !strings.EqualFold(sub, cmdlet) {
			continue
		}
		// Check that it's a whole word.
		if i > 0 {
			prev := command[i-1]
			if prev != ' ' && prev != '\t' && prev != '|' && prev != ';' && prev != '\n' && prev != '&' {
				continue
			}
		}
		end := i + len(cmdlet)
		if end < len(command) {
			next := command[end]
			if next != ' ' && next != '\t' && next != '\n' && next != '\r' {
				continue
			}
		}
		return i
	}
	return -1
}

// extractPSPathArg extracts the value of -Path or -FilePath from a PowerShell
// cmdlet argument tail (the part of the command after the cmdlet name).
func extractPSPathArg(tail string) string {
	for _, flag := range []string{"-FilePath", "-Path"} {
		if path := extractFlagValue(tail, flag); path != "" {
			return path
		}
	}
	return ""
}

// extractFlagValue extracts a flag's value from a command tail. Handles
// quoted and unquoted values, and -Flag=value / -Flag:value syntax.
func extractFlagValue(tail, flag string) string {
	rest := tail
	for {
		idx := findFlag(rest, flag)
		if idx < 0 {
			return ""
		}
		after := rest[idx+len(flag):]
		// Handle -Flag=value or -Flag:value syntax.
		if len(after) > 0 && (after[0] == '=' || after[0] == ':') {
			after = after[1:]
		}
		after = strings.TrimLeftFunc(after, func(r rune) bool {
			return r == ' ' || r == '\t'
		})
		if after == "" {
			rest = rest[idx+len(flag):]
			continue
		}
		// Quoted value.
		if after[0] == '"' || after[0] == '\'' {
			q := after[0]
			end := strings.IndexByte(after[1:], q)
			if end >= 0 {
				return after[1 : 1+end]
			}
			return ""
		}
		// Unquoted value: take until whitespace or end.
		end := strings.IndexFunc(after, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';' || r == '|' || r == '&'
		})
		if end < 0 {
			return after
		}
		return after[:end]
	}
}

// findFlag finds a flag in a command string, matching only when it appears as a
// whole flag (preceded by whitespace, semicolon, pipe, or start-of-string).
func findFlag(s, flag string) int {
	lower := strings.ToLower(s)
	flagLower := strings.ToLower(flag)
	for i := 0; i <= len(lower)-len(flagLower); i++ {
		sub := lower[i : i+len(flagLower)]
		if sub != flagLower {
			continue
		}
		if i > 0 {
			prev := s[i-1]
			if prev != ' ' && prev != '\t' && prev != ';' && prev != '|' && prev != '\n' {
				continue
			}
		}
		return i
	}
	return -1
}

// extractRedirectTargets extracts file paths from shell redirect operators
// (> file, >> file, 2> file, 1> file, &> file, 2>> file, etc.).
func extractRedirectTargets(command string) []string {
	var targets []string
	// Scan for > (redirect) outside quoted spans.
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
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
		if c != '>' {
			continue
		}
		// Skip >> (append) — treat it the same as >.
		if i+1 < len(command) && command[i+1] == '>' {
			i++ // skip second >
		}
		// Extract the target path after >.
		after := strings.TrimLeftFunc(command[i+1:], func(r rune) bool {
			return r == ' ' || r == '\t'
		})
		if after == "" {
			continue
		}
		// Skip fd passthrough (e.g., 2>&1, &>1, 1>&2) but NOT >&file which
		// redirects both stdout+stderr to a file.
		if after[0] == '&' {
			if len(after) > 1 && (after[1] >= '0' && after[1] <= '9') {
				continue // fd passthrough: >&1, >&2
			}
			// ">& file" or ">>& file" — consume the &, extract the path.
			afterPath := strings.TrimLeftFunc(after[1:], func(r rune) bool {
				return r == ' ' || r == '\t'
			})
			path := extractShellWord(afterPath)
			if path != "" && !strings.HasPrefix(path, "/dev/") {
				targets = append(targets, path)
			}
			continue
		}
		// Extract the path.
		path := extractShellWord(after)
		if path != "" && !strings.HasPrefix(path, "/dev/") {
			targets = append(targets, path)
		}
	}
	return targets
}

// extractShellWord returns the first shell word from s (handles quoting).
func extractShellWord(s string) string {
	if s == "" {
		return ""
	}
	if s[0] == '"' || s[0] == '\'' {
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end >= 0 {
			return s[1 : 1+end]
		}
		return s[1:] // unterminated quote — take rest
	}
	end := strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';' || r == '|' || r == '&' || r == '<' || r == '>'
	})
	if end < 0 {
		return s
	}
	return s[:end]
}

// extractTeeTargets extracts file paths from tee [file1 file2 ...].
func extractTeeTargets(command string) []string {
	var targets []string
	rest := command
	for {
		idx := findCmdlet(rest, "tee")
		if idx < 0 {
			break
		}
		tail := rest[idx+3:] // skip "tee"
		tail = strings.TrimLeftFunc(tail, func(r rune) bool {
			return r == ' ' || r == '\t'
		})

		// tee accepts -a/--append flags anywhere; skip them.
		for {
			if strings.HasPrefix(tail, "-a ") || strings.HasPrefix(tail, "--append ") {
				tail = tail[strings.IndexByte(tail, ' ')+1:]
				tail = strings.TrimLeftFunc(tail, func(r rune) bool {
					return r == ' ' || r == '\t'
				})
				continue
			}
			if tail == "-a" || tail == "--append" {
				break // flag at end, no files follow
			}
			break
		}

		// Extract all file arguments (tee can write to multiple files).
		for {
			tail = strings.TrimSpace(tail)
			if tail == "" {
				break
			}
			word := extractShellWord(tail)
			if word == "" {
				break
			}
			if strings.HasPrefix(word, "-") {
				// Next flag — could be -a/--append, skip and continue.
				if word == "-a" || word == "--append" {
					tail = tail[len(word):]
					tail = strings.TrimSpace(tail)
					continue
				}
				break // other flags end the file list
			}
			targets = append(targets, word)
			tail = tail[len(word):]
		}
		rest = rest[idx+3:]
	}
	return targets
}

// extractDdTargets extracts file paths from dd of=file.
func extractDdTargets(command string) []string {
	var targets []string
	rest := command
	for {
		idx := findCmdlet(rest, "dd")
		if idx < 0 {
			break
		}
		tail := rest[idx+2:] // skip "dd"
		// Find of= in the arguments.
		for {
			eqIdx := strings.Index(strings.ToLower(tail), "of=")
			if eqIdx < 0 {
				break
			}
			valStart := eqIdx + 3
			end := strings.IndexFunc(tail[valStart:], func(r rune) bool {
				return r == ' ' || r == '\t' || r == '\n'
			})
			var val string
			if end < 0 {
				val = tail[valStart:]
			} else {
				val = tail[valStart : valStart+end]
			}
			if val != "" {
				targets = append(targets, val)
			}
			tail = tail[valStart:]
		}
		rest = rest[idx+2:]
	}
	return targets
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
