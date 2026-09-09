package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
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
	schemas := a.svc.tools.Schemas()
	if !provider.NativeToolSearchEnabled(a.svc.prov) {
		return schemas
	}
	return provider.ApplyNativeToolSearch(schemas, deferredMCPSchemas(a.svc.tools), a.svc.prov)
}

func deferredMCPSchemas(reg *tool.Registry) []provider.ToolSchema {
	if reg == nil {
		return nil
	}
	var extra []provider.ToolSchema
	for _, name := range reg.AllNames() {
		if !strings.HasPrefix(name, "mcp__") {
			continue
		}
		if reg.ProviderVisible(name) {
			continue
		}
		target, ok := reg.Get(name)
		if !ok {
			continue
		}
		extra = append(extra, provider.ToolSchema{
			Name:        target.Name(),
			Description: target.Description(),
			Parameters:  target.Schema(),
		})
	}
	return extra
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
		return i18n.M.TaskBudget
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
	a.sess.conversation.Add(HostGeneratedUserMessage(a.withTurnPreferences(cause.nudge(state, canSubmitPlan))))
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
