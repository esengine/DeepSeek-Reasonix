package delegation

import (
	"context"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/mutation"
)

func (t *TaskTool) runScheduledProfile(ctx context.Context, plan *profileExecution, environment *environmentRequest, req writeclaim.AcquireRequest, execution string, life *delegationLifecycle, trk *subagentProgressTracker) (string, error) {
	if t.mutations == nil {
		release, err := t.acquireSlot(ctx, req, plan.spec.Sched)
		if err != nil {
			return "", err
		}
		defer release()
		return t.runAdmittedProfile(ctx, plan, environment, execution, life, trk)
	}
	if err := plan.spec.Sched.started(); err != nil {
		return "", err
	}
	endExecution := t.scheduler.TrackExecution(req.Writer)
	defer endExecution()
	return t.runAdmittedProfile(ctx, plan, environment, execution, life, trk)
}

func (t *TaskTool) admitPreparation(ctx context.Context, execution string) (func(), error) {
	capacities := t.scheduler.PhaseCapacity().Preparation
	if t.mutations == nil || len(capacities) == 0 {
		return func() {}, nil
	}
	lease, err := t.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.CapacityOnly, Capacities: capacities,
		SessionID: agent.ParentSession(ctx), ExecutionID: execution, CallID: "task.prepare"})
	if err != nil {
		return nil, err
	}
	return func() { lease.End(mutation.NotExecuted) }, nil
}

func (p *profileExecution) prepareAdmittedRuntime(ctx context.Context, execution string) error {
	if p.task.mutations == nil {
		return nil
	}
	release, err := p.task.admitPreparation(ctx, execution)
	if err != nil {
		return err
	}
	defer release()
	p.registry, err = p.task.childRegistry(&p.spec, p.childDepth, p.writeGrant, p.model)
	if err != nil {
		return err
	}
	return p.prepareTranscript(ctx, execution)
}

func (t *TaskTool) environmentPreparationContext(ctx context.Context) context.Context {
	if t.mutations == nil {
		return ctx
	}
	return isolation.WithPreparationCapacities(ctx, t.scheduler.PhaseCapacity().Preparation)
}

func (t *TaskTool) reserveResident() (func(), error) {
	if t.mutations == nil {
		return func() {}, nil
	}
	return t.scheduler.ReserveResident()
}
