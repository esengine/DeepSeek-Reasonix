package agent

import (
	"errors"
	"fmt"
	"strings"

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

type goalStuckPause struct {
	limit  int
	key    string
	reason string
}

func (e *goalStuckPause) Error() string {
	if e == nil || strings.TrimSpace(e.reason) == "" {
		return "goal paused after a structural no-progress loop; completed work is saved"
	}
	return "goal paused after a structural no-progress loop: " + e.reason
}

type todoStallPause struct {
	rounds int
}

func (e *todoStallPause) Error() string {
	return fmt.Sprintf("paused after %d tool-call rounds without advancing the current todo — the work so far is saved; inspect the blocker or send another message to continue", e.rounds)
}

func isToolLoopPause(err error) bool {
	var maxPause *maxStepsPause
	var stallPause *todoStallPause
	var stuckPause *goalStuckPause
	return errors.As(err, &maxPause) || errors.As(err, &stallPause) || errors.As(err, &stuckPause)
}

// HostProgressSignatures exposes successful evidence identities to the Goal FSM.
func (a *Agent) HostProgressSignatures() []string {
	if a == nil || a.evidence == nil {
		return nil
	}
	return a.evidence.SuccessfulProgressSignaturesSince(0)
}

func (a *Agent) resetStructuralRunGuards() {
	a.stormSig, a.stormCount, a.blockedTurnStreak = "", 0, 0
	a.progress.reset()
}

func (a *Agent) prepareRepeatFailureScope(scoped bool, scopeID string) {
	if !scoped || a.repeatFailureScope != scopeID {
		a.repeatFailureCounts = nil
	} else {
		for sig, failure := range a.repeatFailureCounts {
			if !failure.stateRecheck {
				delete(a.repeatFailureCounts, sig)
			}
		}
	}
	if scoped {
		a.repeatFailureScope = scopeID
	} else {
		a.repeatFailureScope = ""
	}
}

func (a *Agent) stopUnexecutedBoundaryCalls(state *runLoopState, calls []provider.ToolCall, usage *provider.Usage) (error, bool) {
	switch {
	case state.graceRound:
		a.pairUnexecutedGraceCalls(calls, "blocked: the tool-call round budget is exhausted; no more tools will run in this turn")
		return &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey, hostOwned: state.runLimitHostOwned}, true
	case state.goalStuckGraceRound:
		a.pairUnexecutedGraceCalls(calls, "blocked: Goal structural progress guard paused this run; no more tools will run until the user resumes")
		return &goalStuckPause{limit: state.goalStuckLimit, key: state.goalStuckKey, reason: state.goalStuckReason}, true
	case state.recoveryGraceRound:
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			_, _ = ctrl.ConsumeFinalization(a.recoveryTaskID)
		}
		a.pairUnexecutedGraceCalls(calls, "blocked: Auto recovery already paused this turn. Do not call tools; the user will continue in the next message.")
		a.contextManager().ObserveUsage(usage)
		return &RecoveryPauseError{Message: "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction."}, true
	default:
		return nil, false
	}
}

func (a *Agent) trackTodoProgress(state *runLoopState, receiptMark int) error {
	if a.planMode.Load() {
		return nil
	}
	nextProgress, nextTracking := a.canonicalTodoProgress()
	hostProgress := false
	if a.evidence != nil {
		for _, sig := range a.evidence.SuccessfulProgressSignaturesSince(receiptMark) {
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
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(todoProgressNudgeMessage(state.todoStallRounds))})
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
			Text: loopGuardNoticeText(), Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds; asking the assistant to reassess", state.todoStallRounds)})
	}
	if state.todoStallRounds < maxTodoStallRounds {
		return nil
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
		Text: "Task progress stalled; pausing before more tools are called.", Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds after a host reassessment; work is saved and can be resumed", state.todoStallRounds)})
	return &todoStallPause{rounds: state.todoStallRounds}
}

func (a *Agent) armGoalStuckFinalization(state *runLoopState, stuck goalStuckSignal) bool {
	if stuck.reason == "" || state.goalStuckGraceRound {
		return false
	}
	state.goalStuckGraceRound = true
	state.goalStuckLimit, state.goalStuckKey, state.goalStuckReason = stuck.limit, stuck.key, stuck.reason
	nudge := "The Goal is caught in a host-detected no-progress loop. Do not call any more tools. Summarize what was completed, what remains unresolved, and what should change before the user resumes."
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeLoopGuard,
		Text: "Goal progress is structurally stuck; pausing after one summary response.", Detail: stuck.reason})
	return true
}
