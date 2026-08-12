package agent

import (
	"strings"
	"time"

	"reasonix/internal/provider"
)

// missingReasoningWatch is this agent's live view of one incident: whether it
// is active, whether the persisted state already records it, how many healthy
// rounds have run since, and a resolve timestamp whose write failed. The four
// only ever move together, and only observeMissingToolCallReasoning moves them.
type missingReasoningWatch struct {
	active           bool      // gates the one automatic retry, not a user-visible warning
	stateRecorded    bool      // avoids a file transaction on every healthy tool-call turn
	healthyStreak    int       // anti-flapping when no cross-process state dir is configured
	pendingResolveAt time.Time // a resolve whose write failed; outlives SetSession
}

// observeMissingToolCallReasoning classifies a thinking-mode tool-call turn and
// claims the one silent retry its active incident allows. DeepSeek requires
// provider-issued thinking content to be replayed, so a missing value is retried
// once before tools execute; three consecutive healthy rounds then resolve the
// incident and re-arm a future isolated regression (#6259, #7059).
func (a *Agent) observeMissingToolCallReasoning(calls []provider.ToolCall, reasoning string) (missing, shouldRetry bool) {
	if len(calls) == 0 || !provider.WarnOnMissingToolCallReasoning(a.prov) {
		return false, false
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.prov)
	observedAt := time.Now()
	if strings.TrimSpace(reasoning) != "" {
		if a.missingReasoningWarnState == nil {
			if a.missingReasoning.active {
				a.missingReasoning.healthyStreak++
				if a.missingReasoning.healthyStreak >= missingReasoningHealthyResolveStreak {
					a.missingReasoning.active = false
					a.missingReasoning.healthyStreak = 0
				}
			}
			return false, false
		}
		shouldResolve := !a.missingReasoning.stateRecorded || a.missingReasoning.active
		if shouldResolve {
			result := missingReasoningResolveResult{Recorded: true, Resolved: true}
			if pending := a.missingReasoning.pendingResolveAt; !pending.IsZero() {
				result = a.missingReasoningWarnState.resolveAt(fingerprint, pending)
				if result.Recorded {
					a.missingReasoning.pendingResolveAt = time.Time{}
				}
			}
			if result.Recorded {
				result = a.missingReasoningWarnState.resolveAt(fingerprint, observedAt)
			}
			if !result.Recorded {
				if observedAt.After(a.missingReasoning.pendingResolveAt) {
					a.missingReasoning.pendingResolveAt = observedAt
				}
				a.missingReasoning.active = true
				a.missingReasoning.stateRecorded = false
			} else if result.Resolved {
				a.missingReasoning.active = false
				a.missingReasoning.stateRecorded = true
			} else {
				a.missingReasoning.active = true
				a.missingReasoning.stateRecorded = false
			}
		}
		return false, false
	}
	a.missingReasoning.healthyStreak = 0
	if s := a.missingReasoningWarnState; s != nil {
		stateReady := true
		alreadyActive := a.missingReasoning.active
		if pending := a.missingReasoning.pendingResolveAt; !pending.IsZero() {
			result := s.resolveAt(fingerprint, pending)
			stateReady = result.Recorded
			if result.Recorded {
				a.missingReasoning.pendingResolveAt = time.Time{}
				if result.Resolved {
					alreadyActive = false
					a.missingReasoning.active = false
				}
			}
		}
		claimed := stateReady && s.claimAt(fingerprint, observedAt)
		if !claimed || alreadyActive {
			// This exact configuration already attempted recovery for the active
			// incident, so keep the empty-key fallback without doubling requests.
			a.missingReasoning.active = true
			a.missingReasoning.stateRecorded = true
			return true, false
		}
		if !stateReady {
			a.missingReasoning.stateRecorded = false
		}
	} else if a.missingReasoning.active {
		return true, false
	}
	a.missingReasoning.active = true
	if a.missingReasoning.pendingResolveAt.IsZero() {
		a.missingReasoning.stateRecorded = true
	}
	return true, true
}
