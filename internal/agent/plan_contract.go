package agent

import (
	"context"
	"encoding/json"
	"path/filepath"

	"reasonix/internal/evidence"
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
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
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
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
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
			criterion := taskcontract.PlanCriterion{ID: c.ID, Text: c.Text}
			switch {
			case c.Optional:
				facts.Optional = append(facts.Optional, criterion)
			case c.Regression:
				facts.Regressions = append(facts.Regressions, criterion)
			default:
				facts.AcceptanceCriteria = append(facts.AcceptanceCriteria, criterion)
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

// acceptanceCriterionIDs lists the approved plan's criterion ids so a tool call
// can check a citation against the plan the user approved.
func (a *Agent) acceptanceCriterionIDs() []string {
	plan := a.planContractSnapshot()
	if plan == nil {
		return nil
	}
	var ids []string
	for _, step := range plan.Steps {
		for _, c := range step.Acceptance {
			if c.ID != "" {
				ids = append(ids, c.ID)
			}
		}
	}
	return ids
}

// withContractState attaches what a tool call needs to check a claim against the
// approved work: the canonical task list, and the criterion ids a proof may
// cite. They travel together because both answer "does this claim name
// something real".
func (a *Agent) withContractState(ctx context.Context) context.Context {
	ctx = evidence.WithTodoState(ctx, a.CanonicalTodoState())
	return evidence.WithAcceptanceCriteria(ctx, a.acceptanceCriterionIDs())
}

// outstandingPlanCriteria lists what the approved plan still lacks fresh
// evidence for, empty when the turn is unplanned. It is the contract's answer to
// "is this actually done", named criterion by criterion, including proofs that
// went stale under a later mutation.
func (a *Agent) outstandingPlanCriteria() []string {
	if a == nil || a.planContractSnapshot() == nil {
		return nil
	}
	c := a.LiveContract()
	if c == nil {
		return nil
	}
	return c.Outstanding()
}

// mutationEscapesPlan reports whether a pending write touches a path the
// approved plan never named. A plan that named no touchpoints says nothing
// about scope, so nothing escapes it: silence is not a claim that everything is
// out of bounds. Directory containment counts — a plan naming a file implies
// its directory is in play, which is how a test file beside it stays in scope.
func (a *Agent) mutationEscapesPlan(toolName string, args json.RawMessage) bool {
	plan := a.planContractSnapshot()
	if plan == nil {
		return false
	}
	allowed := map[string]bool{}
	for _, step := range plan.Steps {
		for _, p := range append(append([]string{}, step.VerifiedFiles...), step.CandidateFiles...) {
			clean := filepath.Clean(p)
			allowed[clean] = true
			allowed[filepath.Dir(clean)] = true
		}
	}
	if len(allowed) == 0 {
		return false
	}
	for _, p := range evidence.ReceiptFromToolCall(toolName, args, false, true).Paths {
		clean := filepath.Clean(p)
		if !allowed[clean] && !allowed[filepath.Dir(clean)] {
			return true
		}
	}
	return false
}
