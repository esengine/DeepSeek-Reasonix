package delegation

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
)

type profileCompletion struct {
	track     *subagentProgressTracker
	life      *delegationLifecycle
	run       *SubagentRun
	handedOff bool
}

// RunProfileSpec executes one delegated run and settles its actual outcome.
func (t *TaskTool) RunProfileSpec(ctx context.Context, spec ProfileExecSpec) (string, error) {
	if t == nil {
		return "", fmt.Errorf("task tool is not configured")
	}
	exit := &profileCompletion{track: newSubagentProgressTracker(ctx, subSink(ctx))}
	defer func() {
		if !exit.handedOff {
			exit.run.Release()
		}
	}()
	out, value, panicked, err := callProfileRun(func() (string, error) {
		return t.runProfileSpec(ctx, spec, exit)
	}, profilePanicCause)
	if !exit.handedOff {
		if panicked {
			err = errors.Join(err, t.transcripts.SaveFailed(exit.run))
			out = FormatSubagentRunResult("", exit.run, true)
		}
		run := exit.run
		exit.run = nil
		run.Release()
		exit.life.settle(ctx, out, err)
		exit.track.finish(ctx.Err(), err)
	}
	if panicked {
		panic(value)
	}
	return out, err
}

func (t *TaskTool) runBackgroundProfile(ctx context.Context, run *SubagentRun, life *delegationLifecycle, trk *subagentProgressTracker, invoke func() (string, error)) (string, error) {
	out, _, panicked, err := callProfileRun(invoke, func(value any) error {
		return fmt.Errorf("%w\n%s", profilePanicCause(value), debug.Stack())
	})
	if panicked {
		out = FormatSubagentRunResult("", run, true)
		err = errors.Join(err, t.transcripts.SaveFailed(run))
	}
	life.settle(ctx, out, err)
	trk.finish(ctx.Err(), err)
	return out, err
}

// callProfileRun reports a recovered panic before any terminal state is published.
// A normal-return marker also distinguishes a legacy nil-valued panic.
func callProfileRun(invoke func() (string, error), panicError func(any) error) (out string, value any, panicked bool, err error) {
	returned := false
	defer func() {
		value = recover()
		panicked = !returned
		if panicked {
			err = panicError(value)
		}
	}()
	out, err = invoke()
	returned = true
	return
}

func profilePanicCause(value any) error {
	if cause, ok := value.(error); ok {
		return fmt.Errorf("internal error: panic: %w", cause)
	}
	return fmt.Errorf("internal error: panic: %v", value)
}
