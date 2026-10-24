package delegation

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/langpref"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/jobs"
)

func (t *TaskTool) startProfileBackground(ctx context.Context, plan *profileExecution, environment *environmentRequest, req writeclaim.AcquireRequest, execution string, life *delegationLifecycle, trk *subagentProgressTracker, releaseResident func()) (string, error) {
	manager, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background execution is not available in this context")
	}
	releaseStart, err := t.reserveBackgroundStart(ctx, manager)
	if err != nil {
		return "", err
	}
	defer releaseStart()
	// Shared-task callers already receive a transcript reference on submission.
	// An isolated transcript must wait until its admitted environment exists.
	if (environment == nil || environment.mode == "shared") && t.mutations == nil {
		if err := plan.prepareTranscript(ctx, execution); err != nil {
			return "", err
		}
		if err := plan.reserveWriter(ctx); err != nil {
			plan.release()
			return "", err
		}
	}
	ledger := evidence.NewLedger()
	label := firstNonEmpty(plan.spec.Task.Description, plan.spec.Worker.Name, "task")
	ref := ""
	if plan.run != nil && plan.run.Ref != "" {
		ref = "\n" + FormatSubagentReference(plan.run)
	}
	job, err := manager.StartOwnedForSession(jobs.SessionFromContext(ctx), "task", label, func(jobCtx context.Context, _ io.Writer) (result string, err error) {
		defer environment.release()
		defer releaseResident()
		runCtx := evidence.WithLedger(backgroundProfileContext(jobCtx, ctx), ledger)
		defer plan.release()
		defer func() { jobs.PublishEvidence(jobCtx, ledger.Summary()) }()
		defer func() {
			if caught := recover(); caught != nil {
				err = fmt.Errorf("internal error: panic: %v\n%s", caught, debug.Stack())
			}
			life.settle(runCtx, result, err)
			trk.finish(runCtx.Err(), err)
		}()
		result, err = t.runScheduledProfile(runCtx, plan, environment, req, execution, life, trk)
		if err != nil && result == "" {
			result = FormatSubagentRunResult("", plan.run, true)
		}
		return result, err
	})
	if err != nil {
		plan.release()
		return "", err
	}
	queued := ""
	if t.scheduler != nil {
		queued = " It may wait in the session queue until a concurrency/write slot is free."
	}
	return fmt.Sprintf("Started background task %q (%s).%s%s\nIt runs across turns; collect its final answer with wait (or wait will return it once done), and you'll be notified when it finishes.", job.ID, label, queued, ref), nil
}

func (t *TaskTool) reserveBackgroundStart(ctx context.Context, manager *jobs.Manager) (func(), error) {
	if t.scheduler != nil {
		return func() {}, nil
	}
	release, running, ok := manager.ReserveStartForSession(jobs.SessionFromContext(ctx), "task", maxConcurrentBackgroundTasks)
	if !ok {
		if err := manager.AdmissionError(jobs.SessionFromContext(ctx)); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%d background tasks are already running for this session (limit %d); collect their results with wait — or run this sub-task in the foreground — before starting more", running, maxConcurrentBackgroundTasks)
	}
	return release, nil
}

// Transfer execution identity and preferences, never arbitrary host capabilities.
// Cancellation and job publication handles remain owned by the job context.
func backgroundProfileContext(job, caller context.Context) context.Context {
	id, sink, _, _ := agent.CallContext(caller)
	ctx := agent.WithCallContext(carryMutationExecution(job, caller), id, sink, nil, agent.PlanModeFromContext(caller))
	ctx = agent.WithParentSession(ctx, agent.ParentSession(caller))
	ctx = agent.CarryChildComposition(ctx, caller)
	ctx = agent.WithSubagentDepth(ctx, agent.SubagentDepth(caller))
	ctx = agent.WithRequestEffort(ctx, agent.RequestEffortFromContext(caller))
	ctx = agent.WithSubagentImageCandidates(ctx, agent.SubagentImageCandidates(caller))
	ctx = langpref.WithResponseLanguagePreference(ctx, langpref.ResponseLanguageFromContext(caller))
	return langpref.WithReasoningLanguagePreference(ctx, langpref.ReasoningLanguageFromContext(caller))
}
