package agent

import "context"

// acquireSlot queues this run for a session concurrency/write slot and tells its
// policy the moment one is held. That moment is the only place the wait can be
// read from: everything before it the run spent ready and not running, which no
// participant except the scheduler is in a position to notice.
func (t *TaskTool) acquireSlot(ctx context.Context, req AcquireRequest, sched SchedulerPolicy) (func(), error) {
	if t.scheduler == nil {
		return func() {}, sched.started()
	}
	release, err := t.scheduler.Acquire(ctx, req)
	if err != nil {
		return release, err
	}
	// A refused start gives the slot straight back: the run never acted, so
	// holding capacity for it would starve the ones that can.
	if err := sched.started(); err != nil {
		release()
		return func() {}, err
	}
	return release, nil
}

// acquireRequestFor derives the slot request from the spec, and repairs a
// background writer that arrived without a claim: a caller that assembled the
// spec by hand instead of through buildTaskSpec would otherwise queue against
// nothing and run unserialised against every other writer.
func (t *TaskTool) acquireRequestFor(spec *ProfileExecSpec) (AcquireRequest, error) {
	req := AcquireRequest{
		Writer:     !spec.Grant.ReadOnly,
		WritePaths: spec.Grant.WritePaths,
		Nested:     spec.Sched.Nested,
		Priority:   spec.Sched.Priority,
		OnQueued:   spec.Sched.OnQueued,
		Label:      firstNonEmpty(spec.Task.Description, spec.Worker.Name, "task"),
	}
	if !req.Writer || !spec.Grant.WritePaths.Empty() || !spec.Sched.RunInBackground {
		return req, nil
	}
	whole, err := WholeWorkspaceWriteClaim(t.workspaceRoot)
	if err != nil {
		return AcquireRequest{}, err
	}
	req.WritePaths = whole
	spec.Grant.WritePaths = whole
	return req, nil
}

func (p SchedulerPolicy) started() error {
	if p.OnStart == nil {
		return nil
	}
	return p.OnStart()
}
