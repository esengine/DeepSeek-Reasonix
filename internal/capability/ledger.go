package capability

import (
	"strings"
	"sync"
	"time"
)

// Outcome tracks what happened for a routed capability this turn.
type Outcome string

const (
	OutcomePending     Outcome = "pending"
	OutcomeInvoked     Outcome = "invoked"
	OutcomeSucceeded   Outcome = "succeeded"
	OutcomeFailed      Outcome = "failed"
	OutcomeUnavailable Outcome = "unavailable"
	OutcomeDeclined    Outcome = "declined"
)

// LedgerEntry is one turn-scoped capability tracking record.
type LedgerEntry struct {
	ID             string
	Policy         AutoUse
	Reason         string
	Outcome        Outcome
	FailureReason  string
	DeclinedReason string
	InvokedAt      time.Time
	Reminded       bool // prefer: host already issued one retry reminder
}

// Ledger records capability route candidates and host-proven outcomes for one turn.
type Ledger struct {
	mu      sync.Mutex
	entries map[string]*LedgerEntry
	order   []string
}

// NewLedger builds an empty turn-scoped capability ledger.
func NewLedger() *Ledger {
	return &Ledger{entries: map[string]*LedgerEntry{}}
}

// Reset clears the ledger between user turns.
func (l *Ledger) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = map[string]*LedgerEntry{}
	l.order = nil
}

// SeedCandidates records the route decision for this turn.
func (l *Ledger) SeedCandidates(decision RouteDecision) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range decision.Candidates {
		id := c.Entry.ID
		if id == "" {
			continue
		}
		if _, ok := l.entries[id]; ok {
			// Keep strongest policy.
			if rank(c.Policy) > rank(l.entries[id].Policy) {
				l.entries[id].Policy = c.Policy
				l.entries[id].Reason = c.Reason
			}
			continue
		}
		l.entries[id] = &LedgerEntry{
			ID:      id,
			Policy:  c.Policy,
			Reason:  c.Reason,
			Outcome: OutcomePending,
		}
		l.order = append(l.order, id)
	}
}

// MarkInvoked records that the agent called a capability.
func (l *Ledger) MarkInvoked(id string) {
	l.setOutcome(id, OutcomeInvoked, "", "")
}

// MarkSucceeded records a successful capability call.
func (l *Ledger) MarkSucceeded(id string) {
	l.setOutcome(id, OutcomeSucceeded, "", "")
}

// MarkFailed records a failed capability call with host-proven detail.
func (l *Ledger) MarkFailed(id, reason string) {
	l.setOutcome(id, OutcomeFailed, reason, "")
}

// MarkUnavailable records a host-proven unavailable state.
func (l *Ledger) MarkUnavailable(id, reason string) {
	l.setOutcome(id, OutcomeUnavailable, reason, "")
}

// MarkDeclined records a prefer decline with a non-empty reason.
func (l *Ledger) MarkDeclined(id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errEmptyDecline
	}
	l.setOutcome(id, OutcomeDeclined, "", reason)
	return nil
}

// MarkReminded records that prefer was missing once and the host reminded.
func (l *Ledger) MarkReminded(id string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.entries[id]; ok {
		e.Reminded = true
	}
}

func (l *Ledger) setOutcome(id string, outcome Outcome, failReason, declineReason string) {
	if l == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[id]
	if !ok {
		e = &LedgerEntry{ID: id, Policy: AutoUseSuggest, Outcome: OutcomePending}
		l.entries[id] = e
		l.order = append(l.order, id)
	}
	// Terminal outcomes stick; invoked upgrades pending.
	switch e.Outcome {
	case OutcomeSucceeded, OutcomeUnavailable, OutcomeDeclined:
		if outcome == OutcomeSucceeded || outcome == OutcomeUnavailable {
			e.Outcome = outcome
		}
	default:
		e.Outcome = outcome
	}
	if failReason != "" {
		e.FailureReason = failReason
	}
	if declineReason != "" {
		e.DeclinedReason = declineReason
	}
	if outcome == OutcomeInvoked || outcome == OutcomeSucceeded || outcome == OutcomeFailed {
		e.InvokedAt = time.Now()
	}
}

// Snapshot returns a copy of ledger entries in seed order.
func (l *Ledger) Snapshot() []LedgerEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LedgerEntry, 0, len(l.order))
	for _, id := range l.order {
		if e, ok := l.entries[id]; ok {
			out = append(out, *e)
		}
	}
	return out
}

// Get returns one entry by ID.
func (l *Ledger) Get(id string) (LedgerEntry, bool) {
	if l == nil {
		return LedgerEntry{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[strings.TrimSpace(id)]
	if !ok {
		return LedgerEntry{}, false
	}
	return *e, true
}

// GateFailure returns a non-empty reason when final answer must be blocked for
// capability policy. preferMissingAllowReminder is true on the first prefer gap.
type GateFailure struct {
	Reason        string
	PreferRemind  bool
	PreferIDs     []string
	RequireIDs    []string
	UnavailableOK bool // require is host-unavailable; may end with blocker, not success claim
}

// CheckFinalGate evaluates require/prefer policy for the final answer.
func (l *Ledger) CheckFinalGate() GateFailure {
	if l == nil {
		return GateFailure{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var requireMissing, preferMissing, unavailable []string
	var preferRemind []string
	for _, id := range l.order {
		e := l.entries[id]
		if e == nil {
			continue
		}
		switch e.Policy {
		case AutoUseRequire:
			switch e.Outcome {
			case OutcomeSucceeded:
				// ok
			case OutcomeUnavailable:
				unavailable = append(unavailable, id+": "+e.FailureReason)
			default:
				requireMissing = append(requireMissing, id)
			}
		case AutoUsePrefer:
			switch e.Outcome {
			case OutcomeSucceeded, OutcomeDeclined, OutcomeUnavailable:
				// ok
			default:
				if !e.Reminded {
					preferRemind = append(preferRemind, id)
				} else {
					preferMissing = append(preferMissing, id)
				}
			}
		}
	}
	if len(requireMissing) > 0 {
		return GateFailure{
			Reason:     "required capabilities not successfully invoked: " + strings.Join(requireMissing, ", "),
			RequireIDs: requireMissing,
		}
	}
	if len(unavailable) > 0 {
		return GateFailure{
			Reason:        "required capabilities unavailable (host-proven): " + strings.Join(unavailable, "; "),
			UnavailableOK: true,
		}
	}
	if len(preferRemind) > 0 {
		return GateFailure{
			Reason:       "preferred capabilities not yet used; call them or use_capability(action=\"decline\", reason=...): " + strings.Join(preferRemind, ", "),
			PreferRemind: true,
			PreferIDs:    preferRemind,
		}
	}
	if len(preferMissing) > 0 {
		return GateFailure{
			Reason:    "preferred capabilities still unused after reminder; call them or decline with a non-empty reason: " + strings.Join(preferMissing, ", "),
			PreferIDs: preferMissing,
		}
	}
	return GateFailure{}
}

var errEmptyDecline = errString("decline reason must be non-empty")

type errString string

func (e errString) Error() string { return string(e) }
