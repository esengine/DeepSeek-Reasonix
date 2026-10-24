package delegation

import (
	"context"
	"fmt"

	"reasonix/internal/state/checkpoint"
)

func (t *subagentProgressTracker) finishReturning(ctx context.Context, handoff *bool, err *error) {
	if *handoff {
		return
	}
	if p := recover(); p != nil {
		*err = fmt.Errorf("panic: %v", p)
		t.finish(nil, *err)
		panic(p)
	}
	t.finish(ctx.Err(), *err)
}

func releaseUnstartedRun(run *SubagentRun, observer *checkpoint.MutationObserver, writerRegistered bool, taskID string) {
	run.Release()
	if writerRegistered {
		observer.UnregisterWriter(taskID)
	}
}
