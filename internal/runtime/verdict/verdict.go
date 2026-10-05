// Package verdict decides a turn's outcome from host evidence alone
// (docs/design/TRUSTED_EXECUTION.md §4). Evaluate has no input a model writes:
// it never reads a criterion's status, because a todo marked completed or a
// complete_step citation can set that, and a claim may trigger verification
// but never satisfy an obligation (I2). The same inputs give the same result.
package verdict

import (
	"slices"

	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

// Verdict is one obligation's state.
type Verdict string

const (
	Satisfied    Verdict = "satisfied"
	Unsatisfied  Verdict = "unsatisfied"
	Unverifiable Verdict = "unverifiable"
	Stale        Verdict = "stale"
	Owed         Verdict = "owed"
)

// Outcome is the bundle's outcome.
type Outcome string

const (
	Completed  Outcome = "completed"
	Incomplete Outcome = "incomplete"
	Failed     Outcome = "failed"
	Blocked    Outcome = "blocked"
	// NoOutcome is a turn that owes nothing and changed nothing the host saw:
	// there is no evidence it completed anything, so it is not called done.
	NoOutcome Outcome = "none"
)

// Causes an obligation is not satisfied, in the design's failure vocabulary.
const (
	CauseNoVerifier      = "verifier.none"
	CauseNotAttempted    = "evidence.missing"
	CauseStale           = "evidence.stale"
	CauseCheckFailed     = "check.failed"
	CauseHostObligation  = "obligation.outstanding"
	CauseSuppressedCheck = "verifier.suppressed"
	CauseContractOwed    = "contract.criterion_owed"
	CauseSubjectMissing  = "verifier.subject_missing"
)

// Frozen is one accepted contract criterion and what the ledger says of it.
// Identity is the verifier's, so a host obligation naming the same subject is
// recognised as the same debt rather than counted twice.
type Frozen struct {
	ID           string
	Source       string
	Identity     string
	Satisfied    bool
	Unverifiable bool
}

// Obligation is one derived obligation and its verdict.
type Obligation struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Required bool    `json:"required"`
	Verdict  Verdict `json:"verdict"`
	Cause    string  `json:"cause,omitempty"`
}

// Result is every obligation and the outcome they add up to.
type Result struct {
	Outcome     Outcome      `json:"outcome"`
	Obligations []Obligation `json:"obligations"`
}

// Input is what the host knows at seal time. Nothing in it is authored by the
// model: Receipts are host observations, Contract contributes only its
// criteria's identities and verifier shape and its checks' host-derived state,
// and HostObligations are the ledger's outstanding debts.
type Input struct {
	Contract        *taskcontract.Contract
	Receipts        []evidence.Receipt
	HostObligations []evidence.Obligation
	// Blocked is the host-checked conclusion that the task cannot be done as
	// specified.
	Blocked bool
	// Frozen are the accepted contract's criteria, answered from the ledger.
	Frozen []Frozen
	// CoveredCriteria are plan criterion ids a frozen check answers for.
	CoveredCriteria []string
}

// Evaluate derives every obligation's verdict and the outcome.
func Evaluate(in Input) Result {
	var obs []Obligation
	if c := in.Contract; c != nil {
		for _, req := range c.Requirements {
			if slices.Contains(in.CoveredCriteria, req.ID) {
				continue
			}
			obs = append(obs, criterion(req, in.Receipts))
		}
		for _, check := range c.Checks {
			obs = append(obs, checkObligation(check))
		}
	}
	covered := map[string]bool{}
	for _, f := range in.Frozen {
		covered[f.Identity] = true
		obs = append(obs, frozenObligation(f))
	}
	for _, o := range in.HostObligations {
		if coveredByContract(o, covered) {
			continue
		}
		obs = append(obs, Obligation{ID: o.ID, Source: "host:" + string(o.Kind), Required: true, Verdict: Owed, Cause: CauseHostObligation})
	}
	outcome := outcomeOf(obs, in.Blocked)
	if outcome == Completed && len(obs) == 0 && !changedAnything(in.Receipts) {
		outcome = NoOutcome
	}
	return Result{Outcome: outcome, Obligations: obs}
}

// criterion has a host verifier only when the ask is its own evidence: an
// atomic change proven by a successful mutation. Every other criterion waits
// for a frozen verifier, which an accepted contract supplies.
func criterion(req taskcontract.Requirement, receipts []evidence.Receipt) Obligation {
	o := Obligation{ID: "criterion@" + req.ID, Source: "criterion", Required: req.Required}
	if !req.Auto || req.AutoKind != taskcontract.EvidenceMutation {
		o.Verdict, o.Cause = Unverifiable, CauseNoVerifier
		return o
	}
	for _, r := range receipts {
		if r.Success && (r.Mutation || r.Write) {
			o.Verdict = Satisfied
			return o
		}
	}
	o.Verdict, o.Cause = Owed, CauseNotAttempted
	return o
}

// checkObligation reads a check's state, which the contract derives from
// receipts alone: nothing a model calls resolves a check.
func checkObligation(check taskcontract.Check) Obligation {
	o := Obligation{ID: "check@" + check.Command, Source: "check", Required: true}
	if check.Kind == taskcontract.CheckMutation {
		o.ID = "check@mutation"
	}
	switch check.Status {
	case taskcontract.Satisfied:
		o.Verdict = Satisfied
	case taskcontract.Failed:
		o.Verdict, o.Cause = Unsatisfied, CauseCheckFailed
	case taskcontract.Stale:
		o.Verdict, o.Cause = Stale, CauseStale
	case taskcontract.Suppressed:
		o.Verdict, o.Cause = Unverifiable, CauseSuppressedCheck
	default:
		o.Verdict, o.Cause = Owed, CauseNotAttempted
	}
	return o
}

func outcomeOf(obs []Obligation, blocked bool) Outcome {
	if blocked {
		return Blocked
	}
	outcome := Completed
	for _, o := range obs {
		if !o.Required {
			continue
		}
		switch o.Verdict {
		case Satisfied:
		case Unsatisfied:
			return Failed
		default:
			outcome = Incomplete
		}
	}
	return outcome
}

func frozenObligation(f Frozen) Obligation {
	o := Obligation{ID: "contract@" + f.ID, Source: "contract:" + f.Source, Required: true}
	switch {
	case f.Unverifiable:
		o.Verdict, o.Cause = Unverifiable, CauseSubjectMissing
	case f.Satisfied:
		o.Verdict = Satisfied
	default:
		o.Verdict, o.Cause = Owed, CauseContractOwed
	}
	return o
}

// coveredByContract reports a host obligation that owes the same verifier an
// accepted criterion already owes.
func coveredByContract(o evidence.Obligation, covered map[string]bool) bool {
	switch o.Kind {
	case evidence.ObligationBaselineCheck, evidence.ObligationMissingProjectCheck:
		return covered[o.Subject()]
	case evidence.ObligationBaselineTest:
		return covered[o.Subject()]
	}
	return false
}

func changedAnything(receipts []evidence.Receipt) bool {
	return slices.ContainsFunc(receipts, func(r evidence.Receipt) bool { return r.Success && (r.Mutation || r.Write) })
}
