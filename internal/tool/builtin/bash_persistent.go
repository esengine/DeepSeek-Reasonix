package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/persistentshell"
	"reasonix/internal/sandbox"
	"reasonix/internal/shellparse"
	"reasonix/internal/shellrun"
	"reasonix/internal/tool"
)

// shellResetNotice tells the model that session shell state is gone. Without it
// the model keeps issuing relative paths against a working directory that the
// reset already discarded.
const shellResetNotice = "The persistent shell was reset; the next bash call starts from the workspace with a fresh working directory and environment."

func persistEnv(env []string) []string {
	return applyEnvOverrides(env, []string{
		"TERM=dumb",
		"NO_COLOR=1",
		"PAGER=cat",
		"GIT_PAGER=cat",
		"BASH_SILENCE_DEPRECATION_WARNING=1",
	})
}

func (b bash) persistentManager(ctx context.Context) *persistentshell.Manager {
	if m := persistentshell.FromContext(ctx); m != nil {
		return m
	}
	return b.persistent
}

// hasBackgroundStatement reports an explicit `&` background operator. A session
// shell outlives the call, so its background children are neither reaped by
// #3702's process-group cleanup nor kept out of the next command's output: both
// contracts only hold in a one-shot process, so such commands stay there.
func hasBackgroundStatement(command string) bool {
	file, err := shellparse.ParseBash(command)
	if err != nil {
		return false
	}
	background := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if stmt, ok := node.(*syntax.Stmt); ok && stmt.Background {
			background = true
		}
		return !background
	})
	return background
}

func (b bash) shouldUsePersistent(ctx context.Context, p bashParams, sh sandbox.Shell) bool {
	if sh.Kind == sandbox.ShellPowerShell {
		return false
	}
	if !persistentshell.Supports(sh) {
		return false
	}
	if p.RunInBackground || p.PreserveBackgroundProcesses {
		return false
	}
	if len(p.AdditionalWriteDirs) > 0 || strings.TrimSpace(p.SandboxPermissions) != "" {
		return false
	}
	if sh.Kind.IsPOSIX() && hasBackgroundStatement(p.Command) {
		return false
	}
	if sh.Kind == sandbox.ShellPowerShell && powerShellIsolated(p.Command) {
		return false
	}
	m := b.persistentManager(ctx)
	return m != nil && !m.Sealed()
}

func rejectPowerShellChaining(ex *tool.ShellExecution, start time.Time, sh sandbox.Shell, command string) (tool.DetailedResult, error, bool) {
	if sh.SupportsChaining() || (!hasUnquotedSeq(command, "&&") && !hasUnquotedSeq(command, "||")) {
		return tool.DetailedResult{}, nil, false
	}
	ex.State = tool.ShellStateNotRun
	ex.FailurePhase = tool.ShellPhasePreflight
	ex.MutationRisk = tool.ShellMutationNotStarted
	ex.DurationMs = time.Since(start).Milliseconds()
	return tool.DetailedResult{Execution: ex}, fmt.Errorf("this shell is Windows PowerShell, which does not parse '&&' or '||'. " +
		"Sequence with ';' (both run regardless of the first's result), use 'if ($?) { ... }' for " +
		"conditional chaining, or issue the commands as separate calls"), true
}

func (b bash) tryPersistent(ctx context.Context, p bashParams, sh sandbox.Shell, prepared sandbox.Prepared, cmdEnv []string, start time.Time, ex *tool.ShellExecution) (tool.DetailedResult, error, bool) {
	out, runEx, err, used := b.runPersistent(ctx, p, sh, prepared, cmdEnv)
	if !used {
		return tool.DetailedResult{}, nil, false
	}
	mergeRunInto(ex, runEx)
	ex.DurationMs = time.Since(start).Milliseconds()
	return tool.DetailedResult{
		Output:    b.appendWriteHints(ctx, out, err, p, prepared.Wrapped),
		Execution: ex,
	}, err, true
}

func (b bash) runPersistent(ctx context.Context, p bashParams, sh sandbox.Shell, prepared sandbox.Prepared, cmdEnv []string) (string, *tool.ShellExecution, error, bool) {
	if !b.shouldUsePersistent(ctx, p, sh) {
		return "", nil, nil, false
	}
	m := b.persistentManager(ctx)
	// The session-private temporary directory must reach the sandbox profile,
	// not just the child environment: the spec that sets TMPDIR/GOCACHE must
	// also bind (Linux) or allow (Seatbelt) that directory.
	spec := b.specForCall(ctx)
	launch := sandbox.PrepareShellArgs(spec, persistentshell.InteractiveArgv(sh), prepared.SessionTemp)
	if spec.Enforce() && !launch.Wrapped {
		ex := shellrun.DescriptorFromShell(sh)
		ex.State = tool.ShellStateNotRun
		ex.FailurePhase = tool.ShellPhaseLaunch
		ex.MutationRisk = tool.ShellMutationNotStarted
		return "", ex, fmt.Errorf("%s", sandbox.UnavailableMessage()), true
	}
	var progress = shellrun.NewProgressWriter(nil)
	if emit, ok := tool.ProgressFrom(ctx); ok {
		progress = shellrun.NewProgressWriter(emit)
	}
	defer progress.Flush()
	res := m.Run(ctx, persistentshell.Request{
		Argv:     launch.Argv,
		Dir:      b.workDir,
		Env:      applyEnvOverrides(cmdEnv, launch.EnvOverrides),
		Command:  p.Command,
		Timeout:  b.foregroundTimeoutFor(p),
		Shell:    sh,
		Progress: progress,
	})
	if !res.Started && res.Err != nil && !res.Reset {
		var startup *persistentshell.StartupError
		if !errors.As(res.Err, &startup) {
			if sh.Kind == sandbox.ShellPowerShell {
				debugPersistentFallback(sh.Kind.String(), res.Err.Error())
				out, ex, err := b.runForegroundDetailed(ctx, p, sh, prepared.Argv, prepared.Wrapped, cmdEnv)
				return appendSessionDataHint(out, "Persistent PowerShell was unavailable before this command started. This call ran once in an isolated process; its directory and variable changes are not retained."), ex, err, true
			}
			return "", nil, nil, false
		}
	}
	ex := shellrun.DescriptorFromShell(sh)
	ex.State = res.State
	ex.FailurePhase = res.FailurePhase
	code := res.ExitCode
	if res.ExitCodeKnown {
		ex.ExitCode = &code
	}
	if res.State != tool.ShellStateCompleted && res.Output != "" {
		ex.OutputTail = res.Output
		if len(ex.OutputTail) > tool.OutputTailMaxBytes {
			ex.OutputTail = ex.OutputTail[len(ex.OutputTail)-tool.OutputTailMaxBytes:]
		}
	}
	switch res.State {
	case tool.ShellStateCompleted:
		ex.MutationRisk = tool.ShellMutationMayHaveCompleted
	case tool.ShellStateNotRun:
		ex.MutationRisk = tool.ShellMutationNotStarted
	case tool.ShellStateFailed:
		if res.FailurePhase == tool.ShellPhaseLaunch || res.FailurePhase == tool.ShellPhasePreflight {
			ex.MutationRisk = tool.ShellMutationNotStarted
		} else {
			ex.MutationRisk = tool.ShellMutationMayBePartial
		}
	case tool.ShellStateTimedOut, tool.ShellStateCancelled:
		ex.MutationRisk = tool.ShellMutationMayBePartial
	default:
		ex.MutationRisk = tool.ShellMutationUnknown
	}
	out := res.Output
	if res.Reset {
		out = appendSessionDataHint(out, shellResetNotice)
	}
	return out, ex, res.Err, true
}

// Conservative isolation is safe for quoted mentions too; do not parse
// PowerShell background syntax with the Bash parser.
func powerShellIsolated(command string) bool {
	lower := strings.ToLower(command)
	for _, token := range []string{"start-job", "start-threadjob", "start-process", "-asjob"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	var quote rune
	var previous rune
	runes := []rune(command)
	for i, ch := range runes {
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			previous = ch
			continue
		}
		if ch == '&' {
			if i+1 < len(runes) && runes[i+1] == '&' || i > 0 && runes[i-1] == '&' {
				continue
			}
			if previous != 0 && !strings.ContainsRune(";\n|({=", previous) {
				return true
			}
		}
		if ch != ' ' && ch != '\t' && ch != '\r' {
			previous = ch
		}
	}
	return false
}
