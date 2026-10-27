package delegation

import (
	"context"
	"errors"

	"reasonix/internal/tools/jobs"
)

func jobCleanup(release func()) jobs.StartOption {
	return jobs.WithFinalizer(func(jobs.Finalization) error {
		if release != nil {
			release()
		}
		return nil
	})
}

func (t *TaskTool) uninvokedTaskFinalizer(ctx context.Context, run *SubagentRun, life *delegationLifecycle, trk *subagentProgressTracker) jobs.StartOption {
	return jobs.WithFinalizer(func(state jobs.Finalization) error {
		if state.Invoked {
			return nil
		}
		ctxErr := ctx.Err()
		if errors.Is(state.Err, context.Canceled) || errors.Is(state.Err, context.DeadlineExceeded) {
			ctxErr = state.Err
		}
		defer trk.finish(ctxErr, state.Err)
		life.settle(ctx, FormatSubagentRunResult("", run, true), state.Err)
		return t.saveRunTerminal(run, state.Err)
	})
}

func uninvokedFleetFinalizer(ctx context.Context, merger *subagentProgressMerger, parentID string) jobs.StartOption {
	return jobs.WithFinalizer(func(state jobs.Finalization) error {
		defer merger.Close()
		if !state.Invoked {
			merger.directStatus(parentID, terminalProgressPhase(delegationState(ctx, state.Err)))
		}
		return nil
	})
}
