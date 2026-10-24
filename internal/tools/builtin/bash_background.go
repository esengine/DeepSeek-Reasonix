package builtin

import (
	"context"
	"io"
	"os/exec"

	"reasonix/internal/base/proc"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessiontemp"
	"reasonix/internal/tools/jobs"
)

type backgroundShell struct {
	params   bashParams
	shell    sandbox.Shell
	prepared sandbox.Prepared
	env      []string
	temp     *sessiontemp.Lease
	mutation *mutation.Continuation
}

func (b bash) startBackground(ctx context.Context, manager *jobs.Manager, run backgroundShell) (*jobs.Job, error) {
	return manager.StartOwnedForSession(jobs.SessionFromContext(ctx), "bash", commandPreview(run.params.Command), func(jobCtx context.Context, out io.Writer) (string, error) {
		var waitDone <-chan struct{}
		if run.temp != nil {
			defer func() { proc.AfterWait(waitDone, run.temp.Release) }()
		}
		lease, err := run.mutation.Acquire(jobCtx)
		if err != nil {
			return "", err
		}
		defer lease.End(mutation.NotExecuted)
		argv, p, sh := run.prepared.Argv, run.params, run.shell
		cmd := exec.CommandContext(jobCtx, argv[0], argv[1:]...)
		cmd.Dir, cmd.Env, cmd.WaitDelay = b.workDir, run.env, bashWaitDelay
		cmd.Stdout, cmd.Stderr = out, out
		started := func(*exec.Cmd) { lease.End(mutation.Unknown) }
		tracked, runErr := runShellProcess(jobCtx, cmd, sh, p.Command, shouldTrackShellProcess(run.prepared.Wrapped, sh, p.Command, p.PreserveBackgroundProcesses), started)
		waitDone = tracked.WaitCompleted()
		if shouldReapAfterRun(jobCtx, sh, p.Command, p.PreserveBackgroundProcesses) {
			reapShellProcess(cmd, tracked)
		}
		b.writeEgressNote(out, run.prepared.EgressToken)
		return "", jobExit(normalizeBashRunError(jobCtx, runErr, p.PreserveBackgroundProcesses))
	}, jobs.WithPresentation(run.params.Description, run.params.Command))
}
