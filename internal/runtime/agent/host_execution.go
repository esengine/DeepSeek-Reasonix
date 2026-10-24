package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/sessiontemp"
)

type HostExecutionResult struct {
	CallID         string
	ExecutionID    string
	Executed       bool
	Blocked        bool
	RefusalCode    string
	Error          string
	EffectiveName  string
	EffectiveArgs  json.RawMessage
	Execution      *tool.ShellExecution
	Evidence       evidence.ChildEvidenceSummary
	Inputs         []evidence.CheckInputs
	CapturedResult evidence.CriterionResult
}

func (r HostExecutionResult) VerificationSucceeded() bool {
	return r.Executed && !r.Blocked && r.RefusalCode == "" && r.Error == "" && r.Execution != nil &&
		r.Execution.State == tool.ShellStateCompleted && r.Execution.ExitCode != nil && *r.Execution.ExitCode == 0 &&
		r.Execution.Verification == tool.ShellVerificationPassed
}

// HostExecutor owns an ephemeral agent and invokes its ordinary tool pipeline.
// It never samples a provider and never borrows another agent's active turn.
type HostExecutor struct {
	agent       *Agent
	temp        *sessiontemp.Manager
	mu          sync.Mutex
	environment *checkenv.Spec
}

func NewHostExecutor(reg *tool.Registry, opts Options, sink event.Sink, environments ...*checkenv.Spec) *HostExecutor {
	if sink == nil {
		sink = event.Discard
	}
	opts.SessionPath, opts.Jobs, opts.MemoryQueue = "", nil, nil
	opts.SandboxEscapeApprover, opts.ConfigWriteApprover = nil, nil
	m := sessiontemp.New()
	m.Retain()
	host := &HostExecutor{agent: New(nil, reg, sessionstore.NewSession(""), opts, hostExecutionSink{sink}), temp: m}
	if len(environments) != 0 && environments[0] != nil {
		copy := *environments[0]
		copy.Client.Environment = append([]string(nil), environments[0].Client.Environment...)
		host.environment = &copy
	}
	return host
}

func (h *HostExecutor) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.temp != nil {
		h.temp.Release()
		h.temp = nil
	}
}

func (h *HostExecutor) Execute(ctx context.Context, call provider.ToolCall) (result HostExecutionResult) {
	if !h.mu.TryLock() {
		return HostExecutionResult{Blocked: true, RefusalCode: "isolation.busy", Error: "host executor already has an active check"}
	}
	defer h.mu.Unlock()
	var invocation *hostCall
	var inputs *evidence.CheckInputObserver
	defer func() {
		if p := recover(); p != nil {
			result = HostExecutionResult{CallID: call.ID, Executed: invocation != nil && invocation.entered.Load(), Error: fmt.Sprintf("host verification panicked; execution result is unknown: %v", p),
				Execution:   &tool.ShellExecution{Kind: "shell", State: tool.ShellStateFailed, MutationRisk: tool.ShellMutationUnknown},
				ExecutionID: executionID(ctx), Evidence: h.checkEvidence(ctx)}
			h.agent.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: call.ID, Name: call.Name, Err: result.Error, Executed: result.Executed, Execution: toEventShellExecution(result.Execution, 0)}})
		}
		if inputs != nil {
			result.Inputs = inputs.Inputs()
		}
		if invocation != nil && invocation.environment != nil {
			result.Inputs = invocation.environment.Inputs()
			result.CapturedResult = invocation.environment.CapturedOutcome()
			invocation.environment.Close()
		}
	}()
	if h.temp == nil {
		return HostExecutionResult{Blocked: true, RefusalCode: "isolation.unavailable", Error: "host executor is closed"}
	}
	if err := ctx.Err(); err != nil {
		return HostExecutionResult{Error: err.Error()}
	}
	ctx = tool.WithoutHostExecutionCapabilities(ctx)
	ctx = tool.WithoutGoalTurnRecorder(ctx)
	ctx = sandbox.WithoutEscapeApprover(ctx)
	ctx = mutation.WithoutLease(ctx)
	ctx = tool.WithoutShellLaunchObserver(ctx)
	ctx = sessiontemp.WithManager(ctx, h.temp)
	ctx = WithoutRunTimeSink(ctx)
	ctx = context.WithValue(ctx, deliveryExecutionScopeKey{}, DeliveryExecutionScope{})
	ctx = beginExecutionContext(WithRunExecution(ctx, "host_check_"+rand.Text()))
	call.ID = "host_" + rand.Text()
	call = h.agent.prepareCallArguments(call)
	inputs = evidence.NewCheckInputObserver(h.agent.observeRoot)
	invocation = &hostCall{expected: call, inputs: inputs}
	if h.environment != nil {
		invocation.environment = checkenv.NewEnvironment(*h.environment, bashCommandFromArgs(json.RawMessage(call.ExecutionArguments())), executionID(ctx), call.ID)
	}
	ctx = context.WithValue(ctx, hostCallKey{}, invocation)
	h.agent.SetPlanMode(PlanModeFromContext(ctx))
	_, turn, err := h.agent.beginRunTurn(ctx, "Run the host-selected verification check")
	if turn != nil && turn.snapshot != nil {
		defer turn.snapshot.cancel()
	}
	if err != nil {
		return HostExecutionResult{CallID: call.ID, ExecutionID: executionID(ctx), Blocked: true, Error: err.Error()}
	}
	out := h.agent.executeObservedCall(ctx, turn, call)
	if out.checkpointErr != nil {
		out.errMsg += " checkpoint observation: " + out.checkpointErr.Error()
	}
	return HostExecutionResult{CallID: call.ID, ExecutionID: executionID(ctx), Executed: out.executed, Blocked: out.blocked,
		RefusalCode: out.refusalCode, Error: out.errMsg, EffectiveName: out.effective.name,
		EffectiveArgs: append(json.RawMessage(nil), out.effective.args...), Execution: out.execution,
		Evidence: h.checkEvidence(ctx)}
}

func (h *HostExecutor) checkEvidence(ctx context.Context) evidence.ChildEvidenceSummary {
	summary := h.agent.task.ledger.Summary()
	for i := range summary.Receipts {
		if summary.Receipts[i].SourceExecutionID == "" {
			summary.Receipts[i].SourceExecutionID = executionID(ctx)
		}
	}
	return summary
}

type hostCallKey struct{}

type hostCall struct {
	expected    provider.ToolCall
	entered     atomic.Bool
	inputs      *evidence.CheckInputObserver
	environment checkenv.Environment
}

func markHostExecution(ctx context.Context, callID string) {
	if call, _ := ctx.Value(hostCallKey{}).(*hostCall); call != nil && call.expected.ID == callID {
		call.entered.Store(true)
	}
}

func enforceHostCall(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	invocation, _ := ctx.Value(hostCallKey{}).(*hostCall)
	if invocation == nil || plan.call.ID != invocation.expected.ID {
		return toolOutcome{}, false
	}
	expected := invocation.expected
	var before, after any
	if expected.Name == plan.evidenceName && json.Unmarshal([]byte(expected.ExecutionArguments()), &before) == nil &&
		json.Unmarshal(plan.evidenceArgs, &after) == nil && reflect.DeepEqual(before, after) {
		return toolOutcome{}, false
	}
	return toolOutcome{blocked: true, refusalCode: "isolation.check_changed", errMsg: "host-selected verification call was replaced before execution"}, true
}

type hostExecutionSink struct{ next event.Sink }

func (s hostExecutionSink) Emit(e event.Event) {
	if e.Kind == event.ToolDispatch || e.Kind == event.ToolResult {
		e.Tool.Issuer = event.IssuedByHost
	}
	s.next.Emit(e)
}
