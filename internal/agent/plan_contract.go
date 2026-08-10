package agent

import (
	"reasonix/internal/plancontract"
	"reasonix/internal/taskcontract"
)

// SetPlanContract records the approved plan this turn executes, or clears it
// when the turn runs without one. The coordinator sets it before every executor
// run so a turn never inherits the previous turn's plan.
func (a *Agent) SetPlanContract(plan *plancontract.Plan) {
	if a == nil {
		return
	}
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	if plan == nil {
		a.planContract = nil
		return
	}
	copied := *plan
	a.planContract = &copied
}

func (a *Agent) planContractSnapshot() *plancontract.Plan {
	if a == nil {
		return nil
	}
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	if a.planContract == nil {
		return nil
	}
	copied := *a.planContract
	return &copied
}

// planFacts projects a plan onto the contract-relevant facts. The projection
// lives here rather than in either package so taskcontract keeps never seeing a
// plan and plancontract keeps depending on nothing above it.
func planFacts(plan plancontract.Plan) taskcontract.PlanFacts {
	facts := taskcontract.PlanFacts{}
	seen := map[string]bool{}
	for _, step := range plan.Steps {
		for _, c := range step.Acceptance {
			switch {
			case c.Optional:
				facts.Optional = append(facts.Optional, c.Text)
			case c.Regression:
				facts.Regressions = append(facts.Regressions, c.Text)
			default:
				facts.AcceptanceCriteria = append(facts.AcceptanceCriteria, c.Text)
			}
		}
		for _, v := range step.Verification {
			if seen[v.Command] {
				continue
			}
			seen[v.Command] = true
			facts.Verifications = append(facts.Verifications, v.Command)
		}
		if len(step.Risks) > 0 {
			facts.Risky = true
		}
		// Scope is where work is expected, not a claim of fact, so a candidate
		// path belongs in it even though it was never read.
		facts.Touchpoints = appendUnseen(facts.Touchpoints, seen, step.VerifiedFiles)
		facts.Touchpoints = appendUnseen(facts.Touchpoints, seen, step.CandidateFiles)
	}
	return facts
}

func appendUnseen(dst []string, seen map[string]bool, add []string) []string {
	for _, s := range add {
		if s == "" || seen["path:"+s] {
			continue
		}
		seen["path:"+s] = true
		dst = append(dst, s)
	}
	return dst
}
