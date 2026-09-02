package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// stormBreakThreshold is how many times in a row the same tool may fail the same
// way before the loop stops echoing the raw error back and instead returns a
// directive to change approach. Two natural self-corrections are healthy; the
// third identical failure is a death-spiral — the dominant case being a tool call
// whose arguments are truncated at the output-token ceiling, which the model then
// re-emits (re-worded but still over-long), truncating the same way again.
const stormBreakThreshold = 3

// repeatSuccessBreakThreshold is how many identical write-like successes the
// agent allows before refusing another copy in the same user turn. Two gives the
// model room for a natural self-correction; the third repeat is usually a
// no-op/write loop and should be redirected to a different tool or final answer.
const repeatSuccessBreakThreshold = 2

const (
	// todoProgressNudgeRounds is the first adaptive checkpoint. The host asks
	// the model to reassess, but keeps the turn alive so it can recover.
	todoProgressNudgeRounds = 8
	// maxTodoStallRounds is the second Goal-only adaptive checkpoint. It resets
	// the intervention epoch and asks for a new plan without ending the run.
	maxTodoStallRounds = 16
)

func todoProgressNudgeMessage(rounds int) string {
	return fmt.Sprintf("Host progress check: the current todo has produced no new completion, unique read, command, or mutation for %d tool-call rounds. Reassess before using more tools: sign off the current item if it is done, narrow the remaining work without replacing the active item, or explain/ask about a real blocker. Do not repeat reads, commands, or writes just to reset this guard.", rounds)
}

// loopGuardBlockErrMsg is the errMsg carried by a repeat-success loop-guard
// block. applyStormBreaker matches it to arm the final-readiness loop-guard
// pass, since that guard also invites the model to report the blocker.
const loopGuardBlockErrMsg = "blocked by loop guard"

// applyStormBreaker detects a run of zero-progress turns and, past the
// threshold, rewrites the model-facing result (results[0]) into a directive to
// change approach. Two detectors, because a stuck model varies its retries two
// ways. The signature detector keys on each call's (tool, error/blocker) — not
// its args — since a stuck model reworks the arguments cosmetically while
// hitting the same host refusal or failure (see the stormSig field doc). The
// streak detector counts consecutive turns in which every call was blocked,
// regardless of shape: rotating tools, reordering a batch, or a blocker whose
// text varies per attempt escapes the signature but is still zero progress —
// only a host refusal (not a plain error) proves that, so the streak requires
// blocked outcomes. Any success resets both. When a guard fires — or when a
// call in the batch was already blocked by the per-call repeat-success guard —
// the final-readiness loop-guard pass is armed so the model may report the
// blocker (see loopGuardAllowsFinal). The hard maxSteps guard remains the
// ultimate backstop; this just keeps the loop from burning that whole budget
// bouncing off the same host refusals.
func (a *Agent) applyStormBreaker(calls []provider.ToolCall, outcomes []toolOutcome, receiptMark int) intervention {
	allBlocked := len(outcomes) > 0
	for _, outcome := range outcomes {
		if !outcome.blocked {
			allBlocked = false
			break
		}
	}
	if allBlocked {
		a.turn.blockedTurnStreak++
	} else {
		a.turn.blockedTurnStreak = 0
	}
	for _, outcome := range outcomes {
		if outcome.blocked && outcome.errMsg == loopGuardBlockErrMsg {
			a.armLoopGuardPass(receiptMark)
			break
		}
	}

	sig, ok := batchStormSignature(calls, outcomes)
	switch {
	case !ok:
		a.turn.stormSig, a.turn.stormCount = "", 0
	case sig != a.turn.stormSig:
		a.turn.stormSig, a.turn.stormCount = sig, 1
	default:
		a.turn.stormCount++
	}
	stormHit := ok && a.turn.stormCount >= stormBreakThreshold
	if !stormHit && consecutiveNormalizedFailure(calls, outcomes, &a.turn.loop) {
		stormHit = true
		a.turn.stormCount = max(a.turn.stormCount, 2)
	}
	streakHit := allBlocked && a.turn.blockedTurnStreak >= stormBreakThreshold
	if !stormHit && !streakHit {
		return intervention{}
	}

	const blockedAdvice = "Change approach: do not keep retrying a blocked tool by changing the tool, command, or arguments. Respect the permission, plan-mode, hook, or loop-guard blocker; use an already-allowed tool, ask the user for the specific approval or choice if appropriate, or explain the blocker in your final answer."
	var guard, detail string
	if stormHit {
		subject := fmt.Sprintf("%q", calls[0].Name)
		short := calls[0].Name
		if len(calls) > 1 {
			subject = fmt.Sprintf("this batch of %d tool calls", len(calls))
			short = fmt.Sprintf("a batch of %d calls", len(calls))
		}
		anyBlocked := false
		for _, outcome := range outcomes {
			if outcome.blocked {
				anyBlocked = true
				break
			}
		}
		action := "failed"
		advice := "Change approach: if an argument is being truncated, write less in one call and split the work into several smaller calls; otherwise fix the arguments, use a different tool, or explain the blocker in your final answer."
		if anyBlocked {
			action = "been blocked or failed"
			advice = blockedAdvice
		}
		guard = fmt.Sprintf(
			"[loop guard] %s has now %s %d times in a row with the same host response. Re-sending it — even with the wording changed — will not help: the calls keep hitting the same outcome. %s",
			subject, action, a.turn.stormCount, advice)
		detail = fmt.Sprintf(
			"loop guard: %s hit the same host response %d× — nudging the model to change approach",
			short, a.turn.stormCount)
	} else {
		guard = fmt.Sprintf(
			"[loop guard] every tool call in the last %d turns has been blocked by the host (permission, plan mode, hook, or loop guard). Switching tools, reordering calls, or rewording arguments will not help while the blockers stand. %s",
			a.turn.blockedTurnStreak, blockedAdvice)
		detail = fmt.Sprintf(
			"loop guard: every tool call blocked %d turns in a row — nudging the model to change approach",
			a.turn.blockedTurnStreak)
	}
	a.armLoopGuardPass(receiptMark)
	return intervention{
		verdict:  verdictRedirect,
		guidance: guard,
		notice:   noticeFor(event.NoticeCodeLoopGuard, event.LevelInfo, loopGuardNoticeText(), detail),
	}
}

func loopGuardNoticeText() string {
	return "The assistant is not making progress; asking it to change approach."
}

// batchStormSignature returns a per-turn fixation signature — each call's
// (name, error/blocker) in order — and ok=true only when every call errored or
// was blocked. ok=false (any success) means the turn made progress, so the
// caller resets the counter. Keying on the host response rather than the args is
// deliberate: a stuck model reworks the arguments while hitting the same
// response, so identical-args matching would miss the loop.
func batchStormSignature(calls []provider.ToolCall, outcomes []toolOutcome) (string, bool) {
	if len(calls) == 0 {
		return "", false
	}
	var sb strings.Builder
	for i := range calls {
		if outcomes[i].errMsg == "" {
			return "", false
		}
		name := calls[i].Name
		if calls[i].ResolvedName != "" {
			name = calls[i].ResolvedName
		}
		sb.WriteString(name)
		sb.WriteByte(0)
		sb.WriteString(errorCategory(name, outcomes[i].errMsg))
		sb.WriteByte(0)
	}
	return sb.String(), true
}
