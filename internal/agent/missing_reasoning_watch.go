package agent

import (
	"strings"
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
// claims the one silent retry its active incident allows. DeepSeek requires
// provider-issued thinking content to be replayed, so a missing value is retried
// once before tools execute; three consecutive healthy rounds then resolve the
// incident and re-arm a future isolated regression (#6259, #7059).
func (a *Agent) observeMissingToolCallReasoning(calls []provider.ToolCall, reasoning string) (missing, shouldRetry bool) {
	return a.observeMissingAssistantReasoning(provider.Message{
		Role: provider.RoleAssistant, ToolCalls: calls, ReasoningContent: reasoning,
	}, true)
}

// observeMissingAssistantReasoning extends the legacy tool-call watcher to
// provider-executed tool activity while preserving its persisted incident and
// anti-flapping behavior.
func (a *Agent) observeMissingAssistantReasoning(message provider.Message, complete bool) (missing, shouldRetry bool) {
	if !provider.RequiresAssistantReasoningReplay(a.svc.prov, message) || !provider.WarnOnMissingToolCallReasoning(a.svc.prov) {
		return false, false
	}
	reasoning := message.ReasoningContent
	if !complete {
		reasoning = ""
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)
	observedAt := time.Now()
	if strings.TrimSpace(reasoning) != "" {
		if a.svc.warnState == nil {
			if a.sess.missingReasoning.active {
				a.sess.missingReasoning.healthyStreak++
				if a.sess.missingReasoning.healthyStreak >= missingReasoningHealthyResolveStreak {
					a.sess.missingReasoning.active = false
					a.sess.missingReasoning.healthyStreak = 0
				}
			}
			return false, false
		}
		shouldResolve := !a.sess.missingReasoning.stateRecorded || a.sess.missingReasoning.active
		if shouldResolve {
			result := missingReasoningResolveResult{Recorded: true, Resolved: true}
			if pending := a.unwrittenResolve.at; !pending.IsZero() {
				result = a.svc.warnState.resolveAt(fingerprint, pending)
				if result.Recorded {
					a.unwrittenResolve.at = time.Time{}
				}
			}
			if result.Recorded {
				result = a.svc.warnState.resolveAt(fingerprint, observedAt)
			}
			if !result.Recorded {
				if observedAt.After(a.unwrittenResolve.at) {
					a.unwrittenResolve.at = observedAt
				}
				a.sess.missingReasoning.active = true
				a.sess.missingReasoning.stateRecorded = false
			} else if result.Resolved {
				a.sess.missingReasoning.active = false
				a.sess.missingReasoning.stateRecorded = true
			} else {
				a.sess.missingReasoning.active = true
				a.sess.missingReasoning.stateRecorded = false
			}
		}
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
			// incident, so keep the empty-key fallback without doubling requests.
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
