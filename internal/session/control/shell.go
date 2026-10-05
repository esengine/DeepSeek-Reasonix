package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/tools/shellrun"
)

// ShellOption is one interpreter this machine actually has. Path is what the
// probe found rather than a name to look up later, so a host carrying two of
// them offers two rows instead of one ambiguous "bash".
type ShellOption struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Version        string `json:"version,omitempty"`
	SupportsAndAnd bool   `json:"supportsAndAnd"`
	// Prefer is the value SaveShellSettings takes to select this option.
	Prefer string `json:"prefer"`
}

// ShellSettings is the shell tool's interpreter as an editor needs it: what is
// configured, what that resolved to, and what else is installed. Options are
// probed instead of listed from a fixed table — offering a shell the host does
// not have is a switch that breaks every command it accepts.
type ShellSettings struct {
	Prefer    string      `json:"prefer"`
	Path      string      `json:"path,omitempty"`
	Effective ShellOption `json:"effective"`
	// Auto is what detection picks here, so "自动" can name its own outcome.
	Auto     ShellOption   `json:"auto"`
	Options  []ShellOption `json:"options"`
	Platform string        `json:"platform"`
}

// ShellSettings reads the configured interpreter and everything installed
// beside it.
func (c *Controller) ShellSettings() ShellSettings {
	prefer, path := "auto", ""
	if cfg, err := config.Load(); err == nil {
		if p := strings.TrimSpace(cfg.Tools.Shell.Prefer); p != "" {
			prefer = strings.ToLower(p)
		}
		path = strings.TrimSpace(cfg.Tools.Shell.Path)
	}
	auto := sandbox.ResolveShell("", "", nil)
	effective := auto
	if prefer != "auto" || path != "" {
		effective = sandbox.ResolveShell(prefer, path, nil)
	}
	out := ShellSettings{
		Prefer:    prefer,
		Path:      path,
		Effective: shellOption(effective),
		Auto:      shellOption(auto),
		Platform:  shellrun.DescriptorFromShell(auto).Platform,
	}
	for _, sh := range sandbox.DetectShells() {
		out.Options = append(out.Options, shellOption(sh))
	}
	out.Options = oneGitBashPerInstall(out.Options, out.Effective.Path)
	return out
}

// oneGitBashPerInstall keeps a single row per Git for Windows install. Its
// bin/bash.exe is a launcher for usr/bin/bash.exe, so offering both draws two
// "Git Bash" buttons that run the same program. The row matching keep wins, so
// a pinned path still shows as selected.
func oneGitBashPerInstall(opts []ShellOption, keep string) []ShellOption {
	at := map[string]int{}
	out := opts[:0:0]
	for _, o := range opts {
		if o.Name != tool.ShellNameGitBash {
			out = append(out, o)
			continue
		}
		root := gitInstallRoot(o.Path)
		i, seen := at[root]
		if !seen {
			at[root] = len(out)
			out = append(out, o)
			continue
		}
		if strings.EqualFold(o.Path, keep) {
			out[i] = o
		}
	}
	return out
}

func gitInstallRoot(path string) string {
	p := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, tail := range []string{"/usr/bin/bash.exe", "/bin/bash.exe", "/usr/bin/bash", "/bin/bash"} {
		if root, ok := strings.CutSuffix(p, tail); ok {
			return root
		}
	}
	return p
}

// SaveShellSettings persists the interpreter choice after proving it runs: a
// path that cannot execute is refused on the screen that typed it rather than
// on every command afterwards. The caller rebuilds the runtime, because boot
// binds the interpreter into the shell tool while assembling it.
func (c *Controller) SaveShellSettings(prefer, path string) error {
	if err := sandbox.VerifyShell(prefer, path); err != nil {
		return fmt.Errorf("这个 shell 用不了：%w", err)
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetShell(prefer, path); err != nil {
		return err
	}
	return cfg.SaveTo(config.UserConfigPath())
}

func shellOption(sh sandbox.Shell) ShellOption {
	ex := shellrun.DescriptorFromShell(sh)
	opt := ShellOption{
		Name:           ex.Shell,
		Path:           sh.Path,
		Version:        ex.ShellVersion,
		SupportsAndAnd: ex.SupportsAndAnd,
		Prefer:         "bash",
	}
	switch ex.Shell {
	case tool.ShellNamePwsh:
		opt.Prefer = "pwsh"
	case tool.ShellNamePowerShell:
		opt.Prefer = "powershell"
	}
	return opt
}

// shellContextBytes bounds how much of a user command's output goes into the
// conversation; the rest is cut from the middle and the cut is said.
const shellContextBytes = 24 << 10

// ShellRun is how a command the user typed is followed up.
type ShellRun struct {
	// LocalOnly keeps the command the user's own: its output is shown, and
	// neither it nor a reply to it enters the conversation.
	LocalOnly bool
}

// answerShell puts a command the user ran into the conversation and lets the
// model respond to it, the way a line typed to the agent would be. A command
// the user stopped is not followed by a turn, and neither is one run with no
// model to answer it.
func (c *Controller) answerShell(ctx context.Context, command, state string, exit *int, output, errText string) error {
	if c.runner == nil || state == tool.ShellStateCancelled {
		return nil
	}
	input := shellTurnInput(command, exit, output, errText)
	return c.runTurnLoop(ctx, orchestratedTurn{input: input, raw: input, display: "!" + command})
}

func shellTurnInput(command string, exit *int, output, errText string) string {
	var b strings.Builder
	b.WriteString("I ran this command in my terminal:\n<bash-input>" + command + "</bash-input>\n")
	if exit != nil {
		fmt.Fprintf(&b, "<bash-exit-code>%d</bash-exit-code>\n", *exit)
	}
	if errText != "" {
		b.WriteString("<bash-error>" + errText + "</bash-error>\n")
	}
	if len(output) > shellContextBytes {
		half := shellContextBytes / 2
		head, tail := strings.ToValidUTF8(output[:half], ""), strings.ToValidUTF8(output[len(output)-half:], "")
		output = fmt.Sprintf("%s\n[… %d bytes of output cut from the middle …]\n%s", head, len(output)-2*half, tail)
	}
	b.WriteString("<bash-output>\n" + output + "\n</bash-output>")
	return b.String()
}

// shellTimeout is the maximum time a user-invoked "!command" may run. Matches
// the bash tool's timeout so behaviour is consistent across invocation paths.
const shellTimeout = 120 * time.Second

// shellWaitDelay bounds how long cmd.Run() waits after context cancellation for
// the child's pipes to drain, matching the bash tool's WaitDelay.
const shellWaitDelay = 5 * time.Second

func shellCommandPreview(command string) string {
	command = strings.TrimSpace(strings.ReplaceAll(command, "\n", " "))
	const max = 48
	r := []rune(command)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return command
}

// RunShell executes a shell command directly (bypassing the model) and streams
// the output as ToolDispatch/ToolProgress/ToolResult events. It uses the same
// bash-tool infrastructure (shell resolution, timeout) and shares the runGuarded
// lock with model turns — only one can run at a time. User-invoked "!" commands
// run without the OS sandbox (the user typed the command explicitly).
func (c *Controller) RunShell(command string) {
	c.RunShellWith(command, ShellRun{})
}

// RunShellWith is RunShell with how the command is followed up spelled out.
func (c *Controller) RunShellWith(command string, opts ShellRun) {
	command = strings.TrimSpace(command)
	if command == "" {
		c.notice(i18n.M.ShellExecEmpty)
		return
	}
	c.runGuarded(func(ctx context.Context) error {
		sh := c.shell
		if sh.Path == "" {
			sh = sandbox.ResolveShell("", "", nil)
		}
		argv, _ := sandbox.Command(sandbox.Spec{}, sh, command) // false = unsandboxed (user invoked)

		preview := []rune(command)
		if len(preview) > 32 {
			preview = preview[:32]
		}
		id := "shell-" + string(preview)
		diagnosticPreview := shellCommandPreview(command)
		desc := shellrun.DescriptorFromShell(sh)

		c.sink.Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{
				ID:     id,
				Name:   "bash",
				Args:   fmt.Sprintf(`{"command":%q}`, command),
				Issuer: event.IssuedByUser,
				Execution: &event.ShellExecution{
					Kind: desc.Kind, Shell: desc.Shell, ShellVersion: desc.ShellVersion,
					Platform: desc.Platform, SupportsAndAnd: desc.SupportsAndAnd,
					State: tool.ShellStateRunning,
				},
			},
		})

		start := time.Now()
		res := shellrun.RunForeground(ctx, shellrun.Request{
			Argv:           argv,
			Dir:            c.workspaceRoot,
			Timeout:        shellTimeout,
			WaitDelay:      shellWaitDelay,
			CommandPreview: diagnosticPreview,
			ShellKind:      sh.Kind.String(),
			ShellPath:      sh.Path,
			Source:         "user_shell",
			Track:          true,
			Progress: func(chunk string) {
				c.sink.Emit(event.Event{
					Kind: event.ToolProgress,
					Tool: event.Tool{ID: id, Output: chunk},
				})
			},
		})
		durationMs := time.Since(start).Milliseconds()
		ex := &event.ShellExecution{
			Kind: desc.Kind, Shell: desc.Shell, ShellVersion: desc.ShellVersion,
			Platform: desc.Platform, SupportsAndAnd: desc.SupportsAndAnd,
			State: res.State, FailurePhase: res.FailurePhase,
			OutputTail: res.OutputTail, DurationMs: durationMs,
			MutationRisk: tool.ShellMutationNone,
			Verification: tool.ShellVerificationNotVerification,
		}
		if res.ExitCode != nil {
			code := *res.ExitCode
			ex.ExitCode = &code
		}
		switch res.State {
		case tool.ShellStateCompleted:
			ex.MutationRisk = tool.ShellMutationNone
		case tool.ShellStateNotRun:
			ex.MutationRisk = tool.ShellMutationNotStarted
		case tool.ShellStateFailed:
			if res.FailurePhase == tool.ShellPhaseLaunch {
				ex.MutationRisk = tool.ShellMutationNotStarted
			} else {
				ex.MutationRisk = tool.ShellMutationMayBePartial
			}
		case tool.ShellStateTimedOut, tool.ShellStateCancelled:
			ex.MutationRisk = tool.ShellMutationMayBePartial
		}

		errText := ""
		switch res.State {
		case tool.ShellStateCancelled:
			errText = i18n.M.TurnCancelled
		case tool.ShellStateTimedOut:
			errText = fmt.Sprintf(i18n.M.ShellExecTimeoutFmt, shellTimeout)
		case tool.ShellStateFailed, tool.ShellStateNotRun:
			if res.Err != nil {
				errText = fmt.Sprintf(i18n.M.ShellExecFailedFmt, res.Err)
			}
		}
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{
				ID: id, Name: "bash", Output: res.Combined, Err: errText,
				DurationMs: durationMs, Execution: ex, Issuer: event.IssuedByUser,
			},
		})
		if opts.LocalOnly {
			return nil
		}
		return c.answerShell(ctx, command, res.State, ex.ExitCode, res.Combined, errText)
	})
}
