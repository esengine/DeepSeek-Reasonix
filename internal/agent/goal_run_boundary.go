package agent

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// maxStepsPause is a resumable stop after a positive model-round budget.
type maxStepsPause struct {
	steps     int
	key       string
	hostOwned bool
}

func (e *maxStepsPause) Error() string {
	return fmt.Sprintf("paused after %d tool-call rounds (%s) — the work so far is saved; send another message to continue, or set %s higher or to 0 for no limit", e.steps, e.key, e.key)
}

func isToolLoopPause(err error) bool {
	var maxPause *maxStepsPause
	var budgetPause *taskBudgetPause
	return errors.As(err, &maxPause) || errors.As(err, &budgetPause)
}

// HostProgressSignatures exposes successful evidence identities to the Goal FSM.
func (a *Agent) HostProgressSignatures() []string {
	if a == nil || a.task.ledger == nil {
		return nil
	}
	return a.task.ledger.SuccessfulProgressSignaturesSince(0)
}

func (a *Agent) resetStructuralRunGuards() {
	a.turn.stormSig, a.turn.stormCount, a.turn.blockedTurnStreak = "", 0, 0
	a.turn.progress.reset()
}

func (a *Agent) stopUnexecutedBoundaryCalls(state *turnRuntime, calls []provider.ToolCall, usage *provider.Usage) (error, bool) {
	switch {
	case state.graceRound:
		a.pairUnexecutedGraceCalls(calls, "blocked: the tool-call round budget is exhausted; no more tools will run in this turn")
		return a.gracePause(state), true
	case state.recoveryGraceRound:
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			_, _ = ctrl.ConsumeFinalization(a.recovery.taskID)
		}
		a.pairUnexecutedGraceCalls(calls, "blocked: Auto recovery already paused this turn. Do not call tools; the user will continue in the next message.")
		a.contextManager().ObserveUsage(usage)
		return &RecoveryPauseError{Message: "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction."}, true
	default:
		return nil, false
	}
}

// trackTodoProgress advances the stall streak and asks the model to reassess
// once, at the checkpoint. It never ends a run: the zero-evidence ladder and
// the storm breaker already own that decision on the same receipts, and they
// reach it far earlier, so a second stop keyed to a todo only added a way for
// the host to end a turn the user never asked it to end.
func (a *Agent) trackTodoProgress(ctx context.Context, state *turnRuntime, receiptMark int) {
	if a.planMode.Load() {
		return
	}
	nextProgress, nextTracking := a.canonicalTodoProgress()
	hostProgress := false
	if a.task.ledger != nil {
		for _, sig := range a.task.ledger.SuccessfulProgressSignaturesSince(receiptMark) {
			if _, seen := state.seenTodoProgress[sig]; !seen {
				hostProgress = true
				state.seenTodoProgress[sig] = struct{}{}
			}
		}
	}
	switch {
	case !nextTracking, !state.trackingTodoProgress || nextProgress > state.todoProgress || hostProgress:
		state.todoStallRounds = 0
	default:
		state.todoStallRounds++
	}
	state.todoProgress, state.trackingTodoProgress = nextProgress, nextTracking
	if state.todoStallRounds == todoProgressNudgeRounds {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(todoProgressNudgeMessage(state.todoStallRounds))})
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
			Text: loopGuardNoticeText(), Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds; asking the assistant to reassess", state.todoStallRounds)})
	}
	if state.todoStallRounds < maxTodoStallRounds {
		return
	}
	if _, goalScoped := DeliveryExecutionScopeFromContext(ctx); goalScoped {
		rounds := state.todoStallRounds
		state.todoStallRounds = 0
		nudge := fmt.Sprintf("Host progress redirect: the current todo still has no new completion or unique host-observed work after %d tool-call rounds. Re-plan and continue: shrink the active step, switch tools or approach, delegate a focused sub-task, or use update_goal(blocked) only if a user or external condition is the sole blocker. Do not repeat the same calls.", rounds)
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
			Text: loopGuardNoticeText(), Detail: fmt.Sprintf("the current Goal todo made no host-observed progress for %d rounds; resetting the intervention epoch and requiring a new plan", rounds)})
		return
	}
}
