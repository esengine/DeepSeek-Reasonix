package agent

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// landCause is why a turn was told to finalize. kind selects the pause the Run
// ends with, so a host can tell "spent its budget" from "hit the round assert".
type landCause struct {
	kind   string
	axis   string
	detail string
}

// turnFinalizer is implemented only by host-consumed terminal tools owned by
// this package. A successful finalizer carries the turn's result as structured
// data, so no provider-generated prose acknowledgement is needed afterwards.
type turnFinalizer interface {
	finalizesTurn()
}

func (a *Agent) providerToolSchemas() []provider.ToolSchema {
	if a == nil || a.svc.tools == nil || !provider.SupportsTools(a.svc.prov) {
		return []provider.ToolSchema{}
	}
	return a.svc.tools.Schemas()
}

func (a *Agent) finishRequiredAtResponseEnd(ctx context.Context, state *turnRuntime, text string, usage *provider.Usage) (bool, error, bool) {
	if !a.requiresStructuredFinish(ctx) {
		return false, nil, false
	}
	state.pendingFinalAnswer = state.pendingFinalAnswer || hasVisibleFinalAnswer(text)
	a.contextManager().ObserveUsage(usage)
	if state.finishCalls == 1 && state.pendingFinalAnswer {
		return false, nil, true
	}
	cont, err := a.requestProtocolRepair(state, "model ended without the required finish tool call")
	return cont, err, true
}

func (a *Agent) rejectMixedFinishBatch(state *turnRuntime, text string, calls []provider.ToolCall, usage *provider.Usage) (bool, error, bool) {
	if !a.containsFinishCall(calls) || a.finalizerName(calls) == "finish" {
		return false, nil, false
	}
	msg := "blocked: finish must be the only tool call in its batch"
	a.pairUnexecutedGraceCalls(calls, msg)
	state.pendingFinalAnswer = state.pendingFinalAnswer || hasVisibleFinalAnswer(text)
	a.contextManager().ObserveUsage(usage)
	cont, err := a.requestProtocolRepair(state, msg)
	return cont, err, true
}

func (a *Agent) acceptFinishCall(state *turnRuntime, text string, calls []provider.ToolCall) (bool, error) {
	state.finishCalls++
	if state.finishCalls != 1 {
		return false, &ProtocolFailedError{Reason: "finish was called more than once"}
	}
	outcome, ok := finishOutcomeFromArgs(calls[0].Arguments)
	if !ok {
		return a.requestProtocolRepair(state, "finish carried an invalid outcome")
	}
	state.finishOutcome = outcome
	state.pendingFinalAnswer = state.pendingFinalAnswer || hasVisibleFinalAnswer(text)
	if !state.pendingFinalAnswer {
		return a.requestProtocolRepair(state, "finish was called without a visible final answer")
	}
	return false, nil
}

func (a *Agent) repairRejectedFinish(state *turnRuntime, text string, calls []provider.ToolCall, usage *provider.Usage) (bool, error, bool) {
	if a.finalizerName(calls) != "finish" {
		return false, nil, false
	}
	state.pendingFinalAnswer = state.pendingFinalAnswer || hasVisibleFinalAnswer(text)
	a.contextManager().ObserveUsage(usage)
	cont, err := a.requestProtocolRepair(state, "finish was rejected; call it once with a valid outcome after the visible answer")
	return cont, err, true
}

// ProtocolFailedError is a terminal contract failure, not an ordinary model or
// transport failure. The controller maps it to protocol_failed so frontends do
// not present a missing structured boundary as successful completion.
type ProtocolFailedError struct {
	Reason string
}

func (e *ProtocolFailedError) Error() string {
	if e == nil || e.Reason == "" {
		return "turn protocol failed"
	}
	return "turn protocol failed: " + e.Reason
}

func IsProtocolFailed(err error) bool {
	var target *ProtocolFailedError
	return errors.As(err, &target)
}

func (a *Agent) singleTurnFinalizer(calls []provider.ToolCall) bool {
	if a == nil || a.svc.tools == nil || len(calls) != 1 {
		return false
	}
	t, _, ambiguous := a.svc.tools.ResolveCall(calls[0].Name)
	if t == nil || len(ambiguous) > 0 {
		return false
	}
	_, ok := t.(turnFinalizer)
	return ok
}

func (a *Agent) finalizerName(calls []provider.ToolCall) string {
	if !a.singleTurnFinalizer(calls) {
		return ""
	}
	_, canonical, _ := a.svc.tools.ResolveCall(calls[0].Name)
	return canonical
}

func (a *Agent) requiresStructuredFinish(ctx context.Context) bool {
	if a == nil || a.svc.tools == nil || !provider.SupportsTools(a.svc.prov) {
		return false
	}
	t, ok := a.svc.tools.Get("finish")
	if !ok || t == nil {
		return false
	}
	if contextual, ok := t.(tool.ContextualTool); ok {
		return contextual.ProviderVisible(ctx)
	}
	return true
}

func (a *Agent) containsFinishCall(calls []provider.ToolCall) bool {
	if a == nil || a.svc.tools == nil {
		return false
	}
	for _, call := range calls {
		_, canonical, ambiguous := a.svc.tools.ResolveCall(call.Name)
		if canonical == "finish" && len(ambiguous) == 0 {
			return true
		}
	}
	return false
}

func (a *Agent) requestProtocolRepair(state *turnRuntime, reason string) (bool, error) {
	if state.protocolRepairs >= 1 {
		return false, &ProtocolFailedError{Reason: reason}
	}
	state.protocolRepairs++
	a.svc.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn,
		Text:   "The model did not complete the required turn protocol; requesting one repair.",
		Detail: reason,
	})
	prompt := "Protocol repair: finish this turn now. "
	if state.finishCalls == 1 && !state.pendingFinalAnswer {
		prompt += "The finish call has already been accepted, so do not call it again. Provide the visible final answer now."
	} else if state.pendingFinalAnswer {
		prompt += "A visible final answer has already been provided, so do not repeat it. Call finish exactly once as the only tool call with outcome completed, partial, or blocked."
	} else {
		prompt += "Provide the visible final answer and call finish exactly once as the only tool call. If you need the user's answer instead, call ask and do not call finish."
	}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(prompt)})
	return true, nil
}

func (a *Agent) allowsBoundaryTurnFinalizer(ctx context.Context, state *turnRuntime, calls []provider.ToolCall) bool {
	if state == nil || !state.graceRound || !a.singleTurnFinalizer(calls) {
		return false
	}
	_, ok := planSubmissionFromContext(ctx)
	return ok
}

func (a *Agent) successfulTurnFinalizer(ctx context.Context, calls []provider.ToolCall, batch batchExecution) bool {
	if !a.singleTurnFinalizer(calls) || len(batch.outcomes) != 1 {
		return false
	}
	outcome := batch.outcomes[0]
	if outcome.errMsg != "" || outcome.blocked {
		return false
	}
	if a.finalizerName(calls) == "finish" {
		return true
	}
	submission, ok := planSubmissionFromContext(ctx)
	if !ok {
		return false
	}
	_, submitted := submission.Plan()
	return submitted
}

func (c landCause) nudge(state *turnRuntime, submitPlan bool) string {
	close := "Do not call any more tools. Synthesize a final answer from the work already completed: what was accomplished, what remains, and any decision the user should make."
	if submitPlan {
		close = "Do not call any more research tools. Synthesize the evidence already collected into the best executor-ready plan you can, label remaining uncertainty, and call submit_plan now."
	}
	if c.kind == "task_budget" {
		return fmt.Sprintf("This task has reached its %s budget. %s Use the evidence already collected and label what is still uncertain; the user can continue in the next message.", c.axis, close)
	}
	tail := fmt.Sprintf("The user can increase %s or continue in the next turn if more work is needed.", state.runMaxStepsKey)
	return fmt.Sprintf("Your tool-call round limit (%s) has been reached. %s %s", state.runMaxStepsKey, close, tail)
}

func (c landCause) noticeText() string {
	if c.kind == "task_budget" {
		return "This task reached its spend budget; asking for a final answer."
	}
	return toolBudgetNoticeText()
}

// armFinalizationRound is the single place a turn is told to stop using tools.
// The grace round it sets — not the wording — is what enforces that: research
// calls in the next round are paired and refused by stopUnexecutedBoundaryCalls;
// a host-consumed structured finalizer is the only exception.
func (a *Agent) armFinalizationRound(ctx context.Context, state *turnRuntime, cause landCause) {
	if state.graceRound {
		return
	}
	state.graceRound = true
	state.landCause = cause
	_, canSubmitPlan := planSubmissionFromContext(ctx)
	a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(cause.nudge(state, canSubmitPlan))})
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeToolBudget,
		Text: cause.noticeText(), Detail: cause.detail})
}

// gracePause is the resumable stop a finalized turn ends with, chosen by what
// caused the landing.
func (a *Agent) gracePause(state *turnRuntime) error {
	if state.landCause.kind == "task_budget" {
		return &taskBudgetPause{axis: state.landCause.axis, detail: state.landCause.detail}
	}
	return &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey}
}

// taskBudgetPause ends a Run that spent its task budget. The work is saved and
// the next message continues it — with a fresh budget, because the user
// deciding to continue is the approval that a round counter cannot ask for.
type taskBudgetPause struct {
	axis   string
	detail string
}

func (e *taskBudgetPause) Error() string {
	return fmt.Sprintf("paused after reaching this task's %s budget (%s) — the work so far is saved; send another message to continue", e.axis, e.detail)
}
