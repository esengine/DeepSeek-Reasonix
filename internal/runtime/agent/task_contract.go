package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"

	"reasonix/internal/runtime/contract"
	"reasonix/internal/runtime/plancontract"
	"reasonix/internal/runtime/verdict"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/trustedstate"
)

// contractState is what the bundle records about the task's contract.
type contractState struct {
	Record   string               `json:"record,omitempty"`
	Revision int                  `json:"revision,omitempty"`
	Decision contract.Decision    `json:"decision,omitempty"`
	Failure  string               `json:"failure,omitempty"`
	Criteria []contract.Criterion `json:"criteria,omitempty"`
}

// settleContract applies host policy to the criteria the task's host-owned
// sources derive now, against the revision in force. A revision the checkpoint
// names but the store cannot produce is a failure, never a reason to accept a
// fresh one in its place: that would invent an acceptance nobody made.
func (a *Agent) settleContract(ctx context.Context, seal *EvidenceSeal) contractState {
	cur, record, err := a.contractInForce(seal)
	if err != nil {
		return contractState{Failure: trustedstate.FailureCode(err)}
	}
	derived := contract.Derive(contract.Sources{Checks: a.task.checkpoint.BaselineChecks, Tests: a.baselineTestIdentities(), PlanChecks: a.planChecks(), PlanChanges: planChangesOf(a.PlanContract())})
	next, decision := contract.Accept(cur, record, derived, a.contractID())
	if decision == contract.RelaxationRefused && a.userApprovedPlanDrops(cur, derived) {
		next, decision = contract.AcceptRelaxation(*cur, record, derived, userPlanApproval), contract.UserRelaxed
	}
	if decision == contract.Accepted || decision == contract.Tightened || decision == contract.UserRelaxed {
		rec, err := contract.Seal(ctx, seal.Store, seal.Stream, next)
		if err != nil {
			st := contractState{Failure: trustedstate.FailureCode(err)}
			if cur != nil {
				st.Record, st.Revision, st.Criteria = record, cur.Revision, cur.Criteria
			}
			return st
		}
		a.task.contract, a.task.contractRecord = &next, rec
		a.task.checkpoint.Contract = rec
		record = rec
	}
	return contractState{Record: record, Revision: next.Revision, Decision: decision, Criteria: next.Criteria}
}

// contractInForce is the revision the checkpoint names. The cached copy is
// trusted only while it is the one named, so a checkpoint reset for a new scope
// starts a new contract rather than inheriting the last one.
func (a *Agent) contractInForce(seal *EvidenceSeal) (*contract.Contract, string, error) {
	named := a.task.checkpoint.Contract
	if named == "" {
		return nil, "", nil
	}
	if a.task.contract != nil && a.task.contractRecord == named {
		return a.task.contract, named, nil
	}
	c, err := contract.Load(seal.Store, named)
	if err != nil {
		return nil, "", err
	}
	a.task.contract, a.task.contractRecord = &c, named
	return &c, named, nil
}

func (a *Agent) contractID() string {
	if id := a.task.checkpoint.ScopeID; id != "" {
		return id
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "task-" + hex.EncodeToString(b[:])
}

func (a *Agent) baselineTestIdentities() []string {
	out := make([]string, 0, len(a.task.baselineCriteria))
	for _, c := range a.task.baselineCriteria {
		out = append(out, c.Identity())
	}
	return out
}

// frozenResults answers each accepted criterion from the ledger, never from
// the checkpoint's copy of the declaration: that copy lives outside Trusted
// Host State, so reading it would let an edit there speak for the contract.
func (a *Agent) frozenResults(criteria []contract.Criterion) []verdict.Frozen {
	if len(criteria) == 0 {
		return nil
	}
	ledger := a.task.ledger
	checks := planCheckIdentities(a.PlanContract())
	for _, c := range criteria {
		if c.Verifier.Kind == contract.VerifierCommand {
			checks = append(checks, c.Verifier.Identity)
		}
	}
	runs := a.checkRunsOf(checks)
	_, changed := runs.baseline(checks)
	owedTests := map[string]bool{}
	for _, o := range evidence.BaselineTestObligations(a.baselineFacts(), a.mutationEpoch()) {
		owedTests[o.ID] = true
	}
	known := map[string]bool{}
	for _, id := range a.baselineTestIdentities() {
		known[id] = true
	}
	out := make([]verdict.Frozen, 0, len(criteria))
	for _, c := range criteria {
		f := verdict.Frozen{ID: c.ID, Source: c.Source, Identity: c.Verifier.Identity}
		switch c.Verifier.Kind {
		case contract.VerifierCommand:
			at, changedSince := runs.baseline([]string{c.Verifier.Identity})
			f.Satisfied = !changedSince || ledger.HasSuccessfulCommandAfter(c.Verifier.Identity, at)
		case contract.VerifierChange:
			f.Satisfied = changed
		case contract.VerifierTest:
			f.Unverifiable = !known[c.Verifier.Identity]
			f.Satisfied = !f.Unverifiable && !owedTests["baseline_test@"+c.Verifier.Identity]
		default:
			f.Unverifiable = true
		}
		out = append(out, f)
	}
	return out
}

// userPlanApproval names the act that carries a User's acceptance of a plan.
const userPlanApproval = "user:plan_approval"

// planChecks are the verification commands the plan names for its required
// acceptance criteria. The user approved them with the plan, so they are the
// plan's own verifiers rather than checks the model picked.
func (a *Agent) planChecks() []string { return planChecksOf(a.PlanContract()) }

func planChecksOf(plan *plancontract.Plan) []string {
	if plan == nil {
		return nil
	}
	var out []string
	for _, step := range plan.Steps {
		if !slices.ContainsFunc(step.Acceptance, func(c plancontract.Criterion) bool { return !c.Optional }) {
			continue
		}
		for _, v := range step.Verification {
			if id := evidence.VerificationIdentity(strings.TrimSpace(v.Command)); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// userApprovedPlanDrops reports a derivation whose only losses are plan
// checks, under a plan the user approved. Losing a project check or a
// captured test is never the plan's to decide.
func (a *Agent) userApprovedPlanDrops(cur *contract.Contract, derived []contract.Criterion) bool {
	plan := a.PlanContract()
	if cur == nil || plan == nil || !plan.ApprovedByUser {
		return false
	}
	dropped := contract.Dropped(cur, derived)
	return len(dropped) > 0 && !slices.ContainsFunc(dropped, func(c contract.Criterion) bool { return c.Source != contract.SourcePlan })
}

// planCriteriaCovered are the plan's criterion ids whose step names a check
// the contract holds; the frozen check answers for them, so the replayed
// contract must not count them again.
func (a *Agent) planCriteriaCovered(criteria []contract.Criterion) []string {
	plan := a.PlanContract()
	if plan == nil {
		return nil
	}
	held := map[string]bool{}
	for _, c := range criteria {
		if c.Verifier.Kind == contract.VerifierCommand {
			held[c.Verifier.Identity] = true
		}
	}
	var out []string
	for _, step := range plan.Steps {
		named := false
		for _, v := range step.Verification {
			if held[evidence.VerificationIdentity(strings.TrimSpace(v.Command))] {
				named = true
			}
		}
		if !named {
			continue
		}
		for _, c := range step.Acceptance {
			out = append(out, c.ID)
		}
	}
	return out
}

// PlanDropsAcceptedChecks reports whether running plan would drop a check the
// accepted contract took from an earlier plan. Only the User may accept that,
// so the host routes such a plan to approval instead of running it.
func (a *Agent) PlanDropsAcceptedChecks(plan plancontract.Plan) bool {
	if a == nil || a.task.contract == nil || a.task.contractRecord != a.task.checkpoint.Contract {
		return false
	}
	kept := planChecksOf(&plan)
	return slices.ContainsFunc(a.task.contract.Criteria, func(c contract.Criterion) bool {
		if c.Source != contract.SourcePlan {
			return false
		}
		if c.Verifier.Kind == contract.VerifierChange {
			return !planChangesOf(&plan)
		}
		return !slices.Contains(kept, c.Verifier.Identity)
	})
}

// planChangesOf reports a plan that names files its steps are expected to
// touch. Candidate files are the planner's inference of where the work lands;
// files it only read say nothing about a change, so they do not count.
func planChangesOf(plan *plancontract.Plan) bool {
	return plan != nil && slices.ContainsFunc(plan.Steps, func(s plancontract.Step) bool { return len(s.CandidateFiles) > 0 })
}
