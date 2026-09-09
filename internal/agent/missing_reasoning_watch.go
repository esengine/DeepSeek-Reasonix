package agent

import (
	"time"

	"reasonix/internal/provider"
)

// missingReasoningWatch is this conversation's live view of one incident. Only
// observeMissingToolCallReasoning moves these, and they belong to the session:
// replacing the conversation ends the incident being watched.
type missingReasoningWatch struct {
	active        bool // gates the one automatic retry, not a user-visible warning
	stateRecorded bool // avoids a file transaction on every healthy tool-call turn
	healthyStreak int  // anti-flapping when no cross-process state dir is configured
}

// unwrittenResolve is a resolve whose state write failed. It answers to the
// provider configuration rather than to any conversation, so it sits beside
// missingReasoningWarnState instead of in sessionRuntime — a new session
// inherits the debt because the retry it owes is still owed.
type unwrittenResolve struct {
	at time.Time
}

// observeMissingToolCallReasoning classifies a thinking-mode tool-call turn and
// claims one recovery for a strict replay contract. Compatible protocols bypass
// this incident state; healthy strict rounds eventually re-arm recovery.
func (a *Agent) observeMissingToolCallReasoning(calls []provider.ToolCall, reasoning string) (missing, shouldRetry bool) {
	return a.observeMissingAssistantReasoning(provider.Message{
		Role: provider.RoleAssistant, ToolCalls: calls, ReasoningContent: reasoning,
	}, true)
}

// observeMissingAssistantReasoning extends the legacy tool-call watcher to
// provider-executed tool activity while preserving its persisted incident and
// anti-flapping behavior.
func (a *Agent) observeMissingAssistantReasoning(message provider.Message, complete bool) (missing, shouldRetry bool) {
	if provider.AllowsEmptyReasoningFallback(a.svc.prov) || !provider.RequiresAssistantReasoningReplay(a.svc.prov, message) {
		return false, false
	}
	decision := provider.DecideReasoningReplay(a.svc.prov, message, complete)
	replayable := decision == provider.ReplayDirect || decision == provider.ReplayCompatible
	// Strict contracts retain their incident across manual continuation.
	if !provider.WarnOnMissingToolCallReasoning(a.svc.prov) {
		if replayable {
			a.recordHealthyAssistantReasoning(provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov), time.Now())
		}
		if !replayable && !provider.AllowsEmptyReasoningFallback(a.svc.prov) {
			return true, a.claimMissingReasoningIncident(time.Now())
		}
		return false, false
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)
	observedAt := time.Now()
	if replayable {
		a.recordHealthyAssistantReasoning(fingerprint, observedAt)
		return false, false
	}
	a.sess.missingReasoning.healthyStreak = 0
	if s := a.svc.warnState; s != nil {
		stateReady := true
		alreadyActive := a.sess.missingReasoning.active
		if pending := a.unwrittenResolve.at; !pending.IsZero() {
			result := s.resolveAt(fingerprint, pending)
			stateReady = result.Recorded
			if result.Recorded {
				a.unwrittenResolve.at = time.Time{}
				if result.Resolved {
					alreadyActive = false
					a.sess.missingReasoning.active = false
				}
			}
		}
		claimed := stateReady && s.claimAt(fingerprint, observedAt)
		if !claimed || alreadyActive {
			// This exact configuration already attempted recovery for the active
			// incident, so do not grant another regeneration.
			a.sess.missingReasoning.active = true
			a.sess.missingReasoning.stateRecorded = true
			return true, false
		}
		if !stateReady {
			a.sess.missingReasoning.stateRecorded = false
		}
	} else if a.sess.missingReasoning.active {
		return true, false
	}
	a.sess.missingReasoning.active = true
	if a.unwrittenResolve.at.IsZero() {
		a.sess.missingReasoning.stateRecorded = true
	}
	return true, true
}

func (a *Agent) recordHealthyAssistantReasoning(fingerprint string, observedAt time.Time) {
	if a.svc.warnState == nil {
		if a.sess.missingReasoning.active {
			a.sess.missingReasoning.healthyStreak++
			if a.sess.missingReasoning.healthyStreak >= missingReasoningHealthyResolveStreak {
				a.sess.missingReasoning.active = false
				a.sess.missingReasoning.healthyStreak = 0
			}
		}
		return
	}
	if a.sess.missingReasoning.stateRecorded && !a.sess.missingReasoning.active {
		return
	}
	result := a.resolveHealthyAssistantReasoning(fingerprint, observedAt)
	if !result.Recorded {
		if observedAt.After(a.unwrittenResolve.at) {
			a.unwrittenResolve.at = observedAt
		}
		a.sess.missingReasoning.active = true
		a.sess.missingReasoning.stateRecorded = false
		return
	}
	if result.Resolved {
		a.sess.missingReasoning.active = false
		a.sess.missingReasoning.stateRecorded = true
		return
	}
	a.sess.missingReasoning.active = true
	a.sess.missingReasoning.stateRecorded = false
}

func (a *Agent) resolveHealthyAssistantReasoning(fingerprint string, observedAt time.Time) missingReasoningResolveResult {
	if pending := a.unwrittenResolve.at; !pending.IsZero() {
		result := a.svc.warnState.resolveAt(fingerprint, pending)
		if !result.Recorded {
			return result
		}
		a.unwrittenResolve.at = time.Time{}
	}
	return a.svc.warnState.resolveAt(fingerprint, observedAt)
}

func (a *Agent) claimMissingReasoningIncident(observedAt time.Time) bool {
	a.sess.missingReasoning.healthyStreak = 0
	if s := a.svc.warnState; s != nil {
		fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)
		claimed := s.claimAt(fingerprint, observedAt)
		a.sess.missingReasoning.active = true
		a.sess.missingReasoning.stateRecorded = true
		return claimed
	}
	if a.sess.missingReasoning.active {
		return false
	}
	a.sess.missingReasoning.active = true
	a.sess.missingReasoning.stateRecorded = true
	return true
}
