package delegation

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/safety/evidence"
)

type EnvironmentFactory func(context.Context, ProfileExecSpec, *EnvironmentSource) (*ExecutionEnvironment, error)

type ExecutionEnvironment struct {
	ID         string
	Generation string
	Task       *TaskTool
	Spec       ProfileExecSpec
	Evidence   *evidence.Ledger
	Finish     func(context.Context, string, error) (string, error)
}

type environmentPolicy struct {
	mode    string
	prepare EnvironmentFactory
}

func (t *TaskTool) WithEnvironmentFactory(mode string, prepare EnvironmentFactory) *TaskTool {
	t.environment = &environmentPolicy{mode: mode, prepare: prepare}
	return t
}

func (t *TaskTool) RunProfileSpec(ctx context.Context, spec ProfileExecSpec) (string, error) {
	if t == nil {
		return "", fmt.Errorf("task tool is not configured")
	}
	source, err := t.resumeEnvironment(ctx, spec)
	if err != nil {
		return "", err
	}
	owned := true
	defer func() {
		if owned {
			source.release()
		}
	}()
	if source != nil {
		spec.Context.ContinueFrom, spec.Context.ForkFrom = source.Reference, ""
	}
	if source != nil && source.Kind == EnvironmentResume && source.ID != "" && source.ID == t.environmentID {
		owned = false
		return t.runProfileSpec(ctx, spec, nil)
	}
	mode, err := t.executionEnvironment(spec)
	if err != nil {
		return "", err
	}
	if source != nil && source.ID == "" {
		if spec.Sched.Environment == "worktree" || spec.Sched.Environment == "isolated" || (t.environment != nil && t.environment.mode == "isolated") {
			return "", tool.Refusal{Code: codeIsolationUnsupported, Message: "an existing shared task cannot move to a new environment during continuation"}
		}
		mode = "shared"
	} else if source != nil && source.ID != "" {
		mode = "worktree"
	}
	if mode == "shared" {
		if source == nil {
			return t.runProfileSpec(ctx, spec, nil)
		}
		owned = false
		return t.runProfileSpec(ctx, spec, &environmentRequest{mode: mode, source: source})
	}
	if t.environment == nil || t.environment.prepare == nil {
		return "", tool.Refusal{Code: isolation.CodeUnavailable, Message: "isolated task execution is unavailable"}
	}
	spec.Worker.Model, spec.Worker.Effort = t.childRuntimeRef(ctx, spec.Worker.Model, spec.Worker.Effort)
	if spec.Worker.Model == "" && spec.Worker.Effort == "" {
		spec.Worker.Effort = agent.RequestEffortFromContext(ctx)
	}
	spec.Worker.Model, spec.Worker.Effort = t.effectiveIdentity(spec.Worker.Model, spec.Worker.Effort)
	owned = false
	return t.runProfileSpec(ctx, spec, &environmentRequest{mode: mode, source: source})
}

type environmentRequest struct {
	mode   string
	source *EnvironmentSource
}

func (r *environmentRequest) release() {
	if r != nil {
		r.source.release()
	}
}

func (t *TaskTool) prepareEnvironment(ctx context.Context, spec ProfileExecSpec, request *environmentRequest, progress event.Sink) (*ExecutionEnvironment, error) {
	if request.mode == "shared" {
		return nil, nil
	}
	if request.mode == "auto" && !spec.Sched.RunInBackground && !spec.Sched.BackgroundWriter && !spec.Sched.ParallelWriter && t.scheduler.ActiveWriters() <= 1 {
		return nil, nil
	}
	prepared, err := t.environment.prepare(ctx, spec, request.source)
	if err != nil {
		if request.mode != "auto" || ctx.Err() != nil {
			return nil, err
		}
		progress.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Automatic isolation could not prepare this task; using shared transactions. " + err.Error(), Detail: err.Error()})
		return nil, nil
	}
	if prepared == nil || prepared.Task == nil {
		return nil, fmt.Errorf("environment factory returned no task runtime")
	}
	prepared.Task.transcripts = t.transcripts
	prepared.Task.scheduler = t.scheduler
	prepared.Task.environmentID = prepared.ID
	prepared.Task.environmentGeneration = prepared.Generation
	prepared.Task.environmentSource = firstNonEmpty(t.environmentSource, t.workspaceRoot)
	if prepared.Evidence == nil {
		prepared.Evidence = evidence.NewLedger()
	}
	return prepared, nil
}

func (t *TaskTool) executionEnvironment(spec ProfileExecSpec) (string, error) {
	mode := strings.TrimSpace(spec.Sched.Environment)
	if mode == "" && t.environment != nil {
		mode = t.environment.mode
	}
	if mode == "" {
		mode = "shared"
	}
	if mode == "isolated" {
		mode = "worktree"
	}
	if mode != "shared" && mode != "worktree" && mode != "auto" {
		return "", tool.Refusal{Code: codeIsolationUnsupported, Message: "isolation must be auto, shared or worktree"}
	}
	if spec.Grant.ReadOnly {
		return "shared", nil
	}
	if t.environmentID != "" && (t.environment == nil || t.environment.prepare == nil) {
		if spec.Sched.Environment == "worktree" || spec.Sched.Environment == "isolated" {
			return "", tool.Refusal{Code: isolation.CodeUnavailable, Message: "a nested task already runs in an isolated environment; creating another environment requires a nested integration route"}
		}
		return "shared", nil
	}
	return mode, nil
}

type environmentTerminal struct {
	once   sync.Once
	finish func(context.Context, string, error) (string, error)
}

func (e *environmentTerminal) settle(ctx context.Context, result string, err error) (string, error) {
	if e != nil && e.finish != nil {
		e.once.Do(func() { result, err = e.finish(ctx, result, err) })
	}
	return result, err
}

func (e *environmentTerminal) onReturn(ctx context.Context, handoff *bool, result *string, err *error) {
	if !*handoff {
		*result, *err = e.settle(ctx, *result, *err)
	}
}

func (e *environmentTerminal) settleLifecycle(ctx context.Context, life *delegationLifecycle, handoff *bool, result *string, err *error) {
	p := recover()
	if p != nil {
		*err = fmt.Errorf("delegated execution panic: %v", p)
	}
	e.onReturn(ctx, handoff, result, err)
	life.settleOnReturn(ctx, handoff, result, err)
	if p != nil {
		panic(p)
	}
}

func (e *environmentTerminal) settleRun(ctx context.Context, result *string, err *error) {
	if e == nil {
		return
	}
	if p := recover(); p != nil {
		*err = fmt.Errorf("isolated task panic: %v", p)
	}
	*result, *err = e.settle(ctx, *result, *err)
}
