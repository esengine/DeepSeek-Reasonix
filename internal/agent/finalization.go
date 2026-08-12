package agent

import (
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// landCause is why a turn was told to finalize. kind selects the pause the Run
// ends with, so a host can tell "spent its budget" from "hit the round assert".
type landCause struct {
	kind   string
	axis   string
	detail string
}

func (c landCause) nudge(state *turnRuntime) string {
	const close = "Do not call any more tools. Synthesize a final answer from the work already completed: what was accomplished, what remains, and any decision the user should make."
	if c.kind == "task_budget" {
		return fmt.Sprintf("This task has reached its %s budget. %s Use the evidence already collected and label what is still uncertain; the user can continue in the next message.", c.axis, close)
	}
	tail := fmt.Sprintf("The user can increase %s or continue in the next turn if more work is needed.", state.runMaxStepsKey)
	if state.runLimitHostOwned {
		tail = "Use the evidence already collected, label remaining uncertainty, and keep the final answer actionable."
	}
	return fmt.Sprintf("Your tool-call round limit (%s) has been reached. %s %s", state.runMaxStepsKey, close, tail)
}

func (c landCause) noticeText() string {
	if c.kind == "task_budget" {
		return "This task reached its spend budget; asking for a final answer."
	}
	return toolBudgetNoticeText()
}

// armFinalizationRound is the single place a turn is told to stop using tools.
// The grace round it sets — not the wording — is what enforces that: unexecuted
// calls in the next round are paired and refused by stopUnexecutedBoundaryCalls.
func (a *Agent) armFinalizationRound(state *turnRuntime, cause landCause) {
	if state.graceRound {
		return
	}
	state.graceRound = true
	state.landCause = cause
	a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(cause.nudge(state))})
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeToolBudget,
		Text: cause.noticeText(), Detail: cause.detail})
}

// gracePause is the resumable stop a finalized turn ends with, chosen by what
// caused the landing.
func (a *Agent) gracePause(state *turnRuntime) error {
	if state.landCause.kind == "task_budget" {
		return &taskBudgetPause{axis: state.landCause.axis, detail: state.landCause.detail}
	}
	return &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey, hostOwned: state.runLimitHostOwned}
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
