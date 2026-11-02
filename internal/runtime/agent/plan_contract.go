package agent

import (
	"context"
	"encoding/json"
	"path/filepath"

	"reasonix/internal/runtime/plancontract"
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

// withContractState attaches what a tool call needs to check a claim against the
// approved work: the canonical task list, and the criterion ids a proof may
// cite. They travel together because both answer "does this claim name
// something real".
func (a *Agent) withContractState(ctx context.Context) context.Context {
	return evidence.WithTodoState(ctx, a.CanonicalTodoState())
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
