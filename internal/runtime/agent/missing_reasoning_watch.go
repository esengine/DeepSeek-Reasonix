package agent

import (
	"strings"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
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

// reasoningObservation classifies one thinking-mode tool-call turn. The states
// are exclusive so no caller can act on an impossible pair such as "not missing
// but replay it".
type reasoningObservation int

const (
	// reasoningIntact carried thinking content, or the provider never promises it.
	reasoningIntact reasoningObservation = iota
	// reasoningModelSilent was billed no thinking tokens: the model chose not to
	// think this round, which is ordinary between tool calls and not a defect.
	reasoningModelSilent
	// reasoningLostNoReplay was billed thinking tokens whose text never arrived,
	// with this incident's single replay already spent.
	reasoningLostNoReplay
	// reasoningLostReplay is reasoningLostNoReplay with the replay still available.
	reasoningLostReplay
)

// missing reports whether provider-issued thinking content went absent.
func (o reasoningObservation) missing() bool {
	return o == reasoningLostNoReplay || o == reasoningLostReplay
}

func (o reasoningObservation) String() string {
	switch o {
	case reasoningIntact:
		return "intact"
	case reasoningModelSilent:
		return "model-silent"
	case reasoningLostNoReplay:
		return "lost-no-replay"
	case reasoningLostReplay:
		return "lost-replay"
	}
	return "unknown"
}

// classifyTurnReasoning classifies one completed attempt and records the audit
// for a silent model, which is observed but never acted on. Call it before
// finalizeSamplingUsage: the classifier needs the single attempt's billing, not
// the multi-attempt aggregate.
func (a *Agent) classifyTurnReasoning(t streamedTurn) reasoningObservation {
	observed := a.observeMissingToolCallReasoning(t.calls, t.reasoning, usageReasoningTokens(t.usage))
	if observed == reasoningModelSilent {
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningModelSilent})
	}
	return observed
}

// observeMissingToolCallReasoning classifies a thinking-mode tool-call turn and
// claims the one silent replay its active incident allows. Billed thinking
// tokens with no text mean the value was lost in transit and a replay can
// recover it; zero billed tokens mean the model stayed silent, where replaying
// an identical request costs a full prompt to buy nothing (#6259, #7059).
func (a *Agent) observeMissingToolCallReasoning(calls []provider.ToolCall, reasoning string, reasoningTokens int) reasoningObservation {
	if len(calls) == 0 || !provider.WarnOnMissingToolCallReasoning(a.svc.prov) {
		return reasoningIntact
	}
	if strings.TrimSpace(reasoning) == "" && reasoningTokens <= 0 {
		return reasoningModelSilent
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
			return reasoningIntact
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
		return reasoningIntact
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
			return reasoningLostNoReplay
		}
		if !stateReady {
			a.sess.missingReasoning.stateRecorded = false
		}
	} else if a.sess.missingReasoning.active {
		return reasoningLostNoReplay
	}
	a.sess.missingReasoning.active = true
	if a.unwrittenResolve.at.IsZero() {
		a.sess.missingReasoning.stateRecorded = true
	}
	return reasoningLostReplay
}
