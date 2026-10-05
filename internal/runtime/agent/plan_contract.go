package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/runtime/plancontract"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
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

// PlanContract is the plan the current turn runs under, or nil for none.
func (a *Agent) PlanContract() *plancontract.Plan {
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
	plan := a.PlanContract()
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

// outstandingPlanCriteria lists the approved plan's required criteria whose
// step names a check that has not passed since the latest change, or has never
// passed when nothing changed. A criterion no command checks is not held here:
// the host cannot settle it, and a citation would only be the model's word.
func (a *Agent) outstandingPlanCriteria() []string {
	plan := a.PlanContract()
	if a == nil || plan == nil || a.task.ledger == nil {
		return nil
	}
	runs := a.checkRunsOf(planCheckIdentities(plan))
	var out []string
	for _, step := range plan.Steps {
		var failing []string
		for _, v := range step.Verification {
			id := evidence.VerificationIdentity(strings.TrimSpace(v.Command))
			if id == "" {
				continue
			}
			at, changed := runs.baseline([]string{id})
			if !changed {
				at = -1
			}
			if !a.task.ledger.HasSuccessfulCommandAfter(id, at) {
				failing = append(failing, v.Command)
			}
		}
		if len(failing) == 0 {
			continue
		}
		for _, c := range step.Acceptance {
			if !c.Optional {
				out = append(out, fmt.Sprintf("%s: %s — run %s after the latest change", c.ID, c.Text, strings.Join(failing, ", ")))
			}
		}
	}
	return out
}

// appendPlanDeliverableGap holds a turn whose plan names files to change and
// which changed nothing, unless it concluded blocked or is waiting on the user:
// either is a legitimate end that no change could follow. carried is a change
// a restart brought in, which the ledger of this run cannot see.
func (a *Agent) appendPlanDeliverableGap(out *finalReadinessCheck, missing []string, carried bool) []string {
	ledger := a.task.ledger
	plan := a.PlanContract()
	if carried || !planChangesOf(plan) {
		return missing
	}
	checks := planCheckIdentities(plan)
	if _, changed := a.checkRunsOf(checks).baseline(checks); changed || ledger.HasBlockedConclusionAfter(-1) {
		return missing
	}
	if _, gated := ledger.UserGateThisTurn(); gated {
		return missing
	}
	out.applies = true
	out.missingAcceptanceCriteria++
	return append(missing, "the approved plan names files to change and nothing has changed: make the planned change, "+
		"or call conclude_blocked with what stops it, or await_user if it waits on them")
}

// planCheckIdentities lists every verification command the plan names, by
// identity, whether or not a required criterion rests on it.
func planCheckIdentities(plan *plancontract.Plan) []string {
	if plan == nil {
		return nil
	}
	var out []string
	for _, step := range plan.Steps {
		for _, v := range step.Verification {
			if id := evidence.VerificationIdentity(strings.TrimSpace(v.Command)); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// mutationEscapesPlan reports whether a pending write touches a path the
// approved plan never named. A plan that named no touchpoints says nothing
// about scope, so nothing escapes it: silence is not a claim that everything is
// out of bounds. Directory containment counts — a plan naming a file implies
// its directory is in play, which is how a test file beside it stays in scope.
func (a *Agent) mutationEscapesPlan(toolName string, args json.RawMessage) bool {
	plan := a.PlanContract()
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
	for _, p := range evidence.ReceiptFromToolCall(toolName, args, false, evidence.ToolFacts{ReadOnly: true}).Paths {
		clean := filepath.Clean(p)
		if !allowed[clean] && !allowed[filepath.Dir(clean)] {
			return true
		}
	}
	return false
}
