package builtin

import (
	"context"
	"fmt"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/tools/shellrun"
)

// runForegroundDetailed uses the shared shellrun collector so model bash and
// user !command share exit-code / phase / output-tail classification.
func (b bash) runForegroundDetailed(ctx context.Context, p bashParams, sh sandbox.Shell, argv []string, wrapped bool, cmdEnv []string, probe pipeStatusProbe) (string, *tool.ShellExecution, error) {
	ex := shellrun.DescriptorFromShell(sh)
	var progress func(string)
	if emit, ok := tool.ProgressFrom(ctx); ok {
		progress = emit
	}
	track := shouldTrackShellProcess(wrapped, sh, p.Command, p.PreserveBackgroundProcesses)
	res := shellrun.RunForeground(ctx, shellrun.Request{
		Argv:              argv,
		Dir:               b.workDir,
		Env:               cmdEnv,
		Timeout:           b.foregroundTimeout(p.TimeoutSeconds),
		WaitDelay:         bashWaitDelay,
		CommandPreview:    commandPreview(p.Command),
		ShellKind:         sh.Kind.String(),
		ShellPath:         sh.Path,
		Source:            "bash_tool",
		Track:             track,
		PreserveWaitDelay: p.PreserveBackgroundProcesses,
		Progress:          progress,
		SuppressLine:      probe.marker(),
	})
	// Wait only reaped the shell leader, so a lingering child (e.g. `bazel run`'s
	// server) stays in the process group and accumulates into an OOM (#3702).
	// shellrun owns the tool-local timeout, hence the state check for ctx.Err().
	reapCtx := ctx
	if res.State == tool.ShellStateTimedOut || res.State == tool.ShellStateCancelled || ctx.Err() != nil {
		// Force reap on forced stops even when preserve_background_processes is set.
		reapShellProcess(res.Cmd, res.Tracked)
	} else if shouldReapAfterRun(reapCtx, sh, p.Command, p.PreserveBackgroundProcesses) {
		reapShellProcess(res.Cmd, res.Tracked)
	}

	ex.State = res.State
	ex.FailurePhase = res.FailurePhase
	ex.ExitCode = res.ExitCode
	ex.OutputTail = res.OutputTail
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
	runErr := res.Err
	if res.State == tool.ShellStateTimedOut {
		runErr = fmt.Errorf("%w; %s", runErr, b.timeoutExit(p.TimeoutSeconds))
	}
	return probe.harvest(res.Combined, ex), ex, runErr
}
