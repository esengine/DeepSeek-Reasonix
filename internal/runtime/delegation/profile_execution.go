package delegation

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/checkpoint"
)

type profileExecution struct {
	task                                *TaskTool
	spec                                ProfileExecSpec
	registry                            *tool.Registry
	writeGrant                          *writeclaim.WriteGrant
	model, effort, usageModel           string
	provider                            provider.Provider
	pricing                             *provider.Pricing
	contextWindow, maxSteps, childDepth int
	run                                 *SubagentRun
	review                              agent.ReviewReportGrant
	observer                            *checkpoint.MutationObserver
	writerRelease                       func()
	presenceRelease                     func()
	releaseOnce                         sync.Once
	environment                         *environmentRequest
}

func (t *TaskTool) prepareProfileExecution(ctx context.Context, spec ProfileExecSpec, trk *subagentProgressTracker) (*profileExecution, error) {
	p, err := t.prepareProfileIdentity(ctx, spec, trk)
	if err != nil {
		return nil, err
	}
	p.registry, err = t.childRegistry(&p.spec, p.childDepth, p.writeGrant, p.model)
	return p, err
}

func (t *TaskTool) prepareAdmissionProfile(ctx context.Context, spec ProfileExecSpec, trk *subagentProgressTracker, environment *environmentRequest) (*profileExecution, error) {
	if (environment != nil && environment.mode != "shared") || t.mutations != nil {
		return t.prepareProfileIdentity(ctx, spec, trk)
	}
	return t.prepareProfileExecution(ctx, spec, trk)
}

func (t *TaskTool) prepareProfileIdentity(ctx context.Context, spec ProfileExecSpec, trk *subagentProgressTracker) (*profileExecution, error) {
	depth, err := t.nextSubagentDepth(ctx)
	if err != nil {
		return nil, err
	}
	p := &profileExecution{task: t, spec: spec, childDepth: depth, maxSteps: t.childMaxStepsForContext(ctx, spec.Sched.MaxSteps), writeGrant: writeclaim.NewWriteGrant(spec.Grant.WritePaths)}
	p.model, p.effort = t.childRuntimeRef(ctx, spec.Worker.Model, spec.Worker.Effort)
	p.usageModel = t.identifyChild(trk, p.model, p.effort)
	p.provider, p.pricing, p.contextWindow, err = t.resolveSubSessionRuntime(p.model, p.effort)
	if err != nil {
		return nil, fmt.Errorf("sub-agent profile: %w", err)
	}
	return p, nil
}

func (p *profileExecution) prepareTranscript(ctx context.Context, execution string) error {
	if p.run != nil {
		return nil
	}
	p.review = reviewGrantFor(p.spec.Worker, execution)
	agent.AttachReviewReport(p.registry, p.review)
	s := p.spec
	var source *EnvironmentSource
	if p.environment != nil && p.environment.source != nil && p.environment.source.Kind == EnvironmentCopy {
		source = p.environment.source
	}
	run, err := p.task.prepareTranscriptRunWithPrompt(withUpstream(ctx, s.Context.Upstream), p.registry, p.model, p.effort, s.Context.parentSession(ctx), execution, s.Context.executionParent(execution), s.Context.ContinueFrom, s.Context.ForkFrom, s.Worker.SystemPrompt, s.Worker.Kind, s.Worker.Name, source)
	if err != nil {
		return err
	}
	p.run = run
	if p.task.mutationObserver != nil {
		p.observer = p.task.mutationObserver.CloneForSubagent(subagentRecoveryTaskID(ctx, run.Ref), p.task.mutationObserver.OwnershipTurn(), p.backgroundWriter())
	}
	return nil
}

func (p *profileExecution) backgroundWriter() bool {
	return (p.spec.Sched.RunInBackground || p.spec.Sched.BackgroundWriter) && !p.spec.Grant.ReadOnly
}

func (p *profileExecution) reserveWriter(ctx context.Context) error {
	if !p.backgroundWriter() || p.observer == nil || p.writerRelease != nil {
		return nil
	}
	id := subagentRecoveryTaskID(ctx, p.run.Ref)
	if err := p.observer.RegisterWriter(id, "background_subagent", p.observer.OwnershipTurn()); err != nil {
		return err
	}
	p.writerRelease = func() { p.observer.UnregisterWriter(id) }
	return nil
}

func (p *profileExecution) release() {
	p.releaseOnce.Do(func() {
		p.environment.release()
		if p.presenceRelease != nil {
			defer p.presenceRelease()
		}
		if p.writerRelease != nil {
			p.writerRelease()
		}
		if p.run != nil {
			p.run.Release()
		}
	})
}

func (t *TaskTool) runAdmittedProfile(ctx context.Context, plan *profileExecution, request *environmentRequest, execution string, life *delegationLifecycle, trk *subagentProgressTracker) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var terminal *environmentTerminal
	defer func() {
		caught := recover()
		if caught != nil {
			err = fmt.Errorf("delegated execution panic: %v", caught)
		}
		result, err = terminal.settle(ctx, result, err)
		if caught != nil && terminal == nil {
			panic(caught)
		}
	}()
	if request != nil && request.mode != "shared" {
		prepared, prepareErr := t.prepareEnvironment(t.environmentPreparationContext(ctx), plan.spec, request, trk.wrap())
		if prepareErr != nil {
			return "", prepareErr
		}
		if prepared != nil {
			terminal = &environmentTerminal{finish: prepared.Finish}
			ctx = evidence.WithLedger(ctx, prepared.Evidence)
			plan, err = prepared.Task.prepareAdmissionProfile(ctx, prepared.Spec, trk, nil)
			if err != nil {
				return "", err
			}
		} else {
			plan, err = t.prepareAdmissionProfile(ctx, plan.spec, trk, nil)
			if err != nil {
				return "", err
			}
		}
	}
	plan.environment = request
	defer plan.release()
	if err := plan.enterExecutionPresence(ctx); err != nil {
		return "", err
	}
	if err := plan.prepareAdmittedRuntime(ctx, execution); err != nil {
		return "", err
	}
	return plan.execute(ctx, execution, life, trk, terminal)
}

func (p *profileExecution) execute(ctx context.Context, execution string, life *delegationLifecycle, trk *subagentProgressTracker, terminal *environmentTerminal) (result string, err error) {
	ctx = agent.WithRunExecution(ctx, execution)
	defer p.release()
	defer p.capturePanic(&result, &err)
	if err := p.prepareTranscript(ctx, execution); err != nil {
		return "", err
	}
	if err := p.reserveWriter(ctx); err != nil {
		return "", err
	}
	if err := p.task.beginExecution(ctx, life, p.spec.Context.Ephemeral, trk, p.run); err != nil {
		return "", err
	}
	p.environment.release()
	answer, err := p.runSession(ctx, trk.wrap(), terminal)
	if err != nil {
		if p.spec.Sched.RunInBackground {
			return FormatSubagentRunResult("", p.run, true), errors.Join(err, p.task.saveRunTerminal(p.run, err))
		}
		return "", p.task.endedRunError(p.run, err)
	}
	if err := p.task.transcripts.SaveCompleted(p.run); err != nil {
		return "", errors.Join(err, p.task.transcripts.SaveFailed(p.run))
	}
	if p.run.Ref != "" {
		return FormatSubagentRunResult(answer, p.run, false), nil
	}
	return GuardSubagentHostDecisionText(answer), nil
}

func (p *profileExecution) capturePanic(result *string, err *error) {
	if caught := recover(); caught != nil {
		*err = fmt.Errorf("internal error: panic: %v\n%s", caught, debug.Stack())
		if p.run != nil {
			*err = errors.Join(*err, p.task.transcripts.SaveFailed(p.run))
			*result = FormatSubagentRunResult("", p.run, true)
		}
		if !p.spec.Sched.RunInBackground {
			panic(caught)
		}
	}
}

func (p *profileExecution) runSession(ctx context.Context, sink event.Sink, terminal *environmentTerminal) (answer string, err error) {
	ctx = p.task.withMutationExecution(ctx, p.run)
	defer terminal.settleRun(ctx, &answer, &err)
	ctx, joinJobs := p.ownExecutionJobs(ctx, sink)
	defer joinJobs(&err)
	id := subagentRecoveryTaskID(ctx, p.run.Ref)
	s := p.spec
	if s.Grant.ReadOnly {
		return p.task.runReadOnlySubSession(withUpstream(ctx, s.Context.Upstream), s.Task.Objective, p.registry, sink, p.maxSteps, p.provider, p.pricing, p.contextWindow, p.run.Session, p.childDepth, id, p.usageModel, p.observer, "read_only_"+s.Worker.Kind, p.review)
	}
	runCtx := p.task.withChildEffort(withUpstream(writeclaim.WithSubagentWriteGrant(ctx, p.writeGrant), s.Context.Upstream), ctx, p.model, p.effort)
	return p.task.runSubSession(runCtx, s.Task.Objective, p.registry, sink, p.maxSteps, p.provider, p.pricing, p.contextWindow, p.run.Session, p.childDepth, id, p.usageModel, p.observer, s.Worker.Kind, p.review, p.task.observeRootFor(p.backgroundWriter(), p.writeGrant))
}
