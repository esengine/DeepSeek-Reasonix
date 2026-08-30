package agent

import (
	"context"
	"strings"
	"time"

	"reasonix/internal/provider"
)

func (a *Agent) withMissingReasoningFallback(ctx context.Context) context.Context {
	if a.sess.missingReasoning.fallbackActive && provider.SupportsMissingReasoningFallback(a.svc.prov) {
		return provider.WithMissingReasoningFallback(ctx)
	}
	return ctx
}

// missingReasoningWatch is this conversation's live view of one incident. Only
// observeMissingToolCallReasoning moves these, and they belong to the session:
// replacing the conversation ends the incident being watched.
type missingReasoningWatch struct {
	active        bool // gates the one automatic retry, not a user-visible warning
	stateRecorded bool // avoids a file transaction on every healthy tool-call turn
	healthyStreak int  // anti-flapping when no cross-process state dir is configured
	// fallbackActive keeps a provider-declared recovery mode stable for the
	// remainder of this conversation. Re-enabling reasoning inside a tool loop
	// that already committed a no-reasoning turn would make its history invalid.
	fallbackActive bool
	// probeClaimedAt identifies this session as the sole cross-process half-open
	// owner. It stays in normal thinking mode until it proves three healthy tool
	// rounds, fails once, or its persisted lease is superseded.
	probeClaimedAt time.Time
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
	// Disabled-thinking fallback intentionally emits no replayable reasoning.
	// Feeding those expected responses back into the incident watcher would
	// reset health progress and rewrite the state file on every tool round.
	if a.sess.missingReasoning.fallbackActive {
		return false, false
	}
	if !provider.RequiresAssistantReasoningReplay(a.svc.prov, message) {
		return false, false
	}
	reasoning := message.ReasoningContent
	if !complete {
		reasoning = ""
	}
	// Persist the incident for strict and empty-fallback protocols alike. Strict
	// providers used to bypass this state and pay for the same exact retry on
	// every new Run; the shared incident now acts as the recovery circuit.
	if !provider.WarnOnMissingToolCallReasoning(a.svc.prov) {
		if strings.TrimSpace(reasoning) == "" && !provider.AllowsEmptyReasoningFallback(a.svc.prov) {
			return true, a.claimMissingReasoningIncident(time.Now())
		}
		return false, false
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)
	observedAt := time.Now()
	if strings.TrimSpace(reasoning) != "" {
		a.recordHealthyAssistantReasoning(fingerprint, observedAt)
		return false, false
	}
	a.sess.missingReasoning.healthyStreak = 0
	if !a.sess.missingReasoning.probeClaimedAt.IsZero() {
		// A half-open failure skips the correlated byte-identical replay. The run
		// loop immediately takes the verified fallback, spending at most one
		// additional normal request for this probe window.
		a.sess.missingReasoning.active = true
		a.sess.missingReasoning.stateRecorded = true
		return true, false
	}
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
	if !result.ProbeClaimedAt.IsZero() {
		a.sess.missingReasoning.probeClaimedAt = result.ProbeClaimedAt
	} else if result.Recorded {
		// The persisted lease was resolved or superseded. This response remains
		// valid for the current tool loop; the next Run obtains a fresh decision.
		a.sess.missingReasoning.probeClaimedAt = time.Time{}
	}
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
		a.sess.missingReasoning.probeClaimedAt = time.Time{}
		return
	}
	a.sess.missingReasoning.active = true
	a.sess.missingReasoning.stateRecorded = false
}

func (a *Agent) resolveHealthyAssistantReasoning(fingerprint string, observedAt time.Time) missingReasoningResolveResult {
	if probeClaimedAt := a.sess.missingReasoning.probeClaimedAt; !probeClaimedAt.IsZero() {
		return a.svc.warnState.resolveProbeAt(fingerprint, probeClaimedAt, observedAt)
	}
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

// beginMissingReasoningRecovery chooses fallback or claims the one half-open
// normal probe before history projection. Keeping the decision session-local
// makes the provider-visible request shape stable for the resulting tool loop.
func (a *Agent) beginMissingReasoningRecovery() bool {
	if a == nil || a.sess.missingReasoning.fallbackActive ||
		!a.sess.missingReasoning.probeClaimedAt.IsZero() || !provider.SupportsMissingReasoningFallback(a.svc.prov) {
		return a != nil && a.sess.missingReasoning.fallbackActive
	}
	if a.svc.warnState == nil {
		return false
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)
	decision := a.svc.warnState.claimRecoveryModeAt(fingerprint, time.Now())
	if decision.Mode == missingReasoningRecoveryProbe {
		a.sess.missingReasoning.active = true
		a.sess.missingReasoning.stateRecorded = true
		a.sess.missingReasoning.probeClaimedAt = decision.ProbeClaimedAt
		return false
	}
	if decision.Mode != missingReasoningRecoveryFallback {
		return false
	}
	a.sess.missingReasoning.active = true
	a.sess.missingReasoning.stateRecorded = true
	a.sess.missingReasoning.fallbackActive = true
	return true
}

func (a *Agent) activateMissingReasoningFallback() bool {
	if a == nil || !provider.SupportsMissingReasoningFallback(a.svc.prov) {
		return false
	}
	a.sess.missingReasoning.active = true
	a.sess.missingReasoning.fallbackActive = true
	if a.svc.warnState != nil {
		fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)
		observedAt := time.Now()
		if probeClaimedAt := a.sess.missingReasoning.probeClaimedAt; !probeClaimedAt.IsZero() {
			a.sess.missingReasoning.stateRecorded = a.svc.warnState.failProbeAt(fingerprint, probeClaimedAt, observedAt)
		} else {
			a.sess.missingReasoning.stateRecorded = a.svc.warnState.openFallbackAt(fingerprint, observedAt)
		}
	}
	a.sess.missingReasoning.probeClaimedAt = time.Time{}
	return true
}
